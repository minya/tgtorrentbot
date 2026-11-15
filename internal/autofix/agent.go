// Package autofix lets Claude fix a completed download: mojibake tags,
// release-junk folder names. Claude calls the media tools of
// internal/mediatools through an in-process MCP session, so every guard of
// the tools applies, and each run is confined to the completed item
// (scopeGuard). The bot's completion checker submits the downloads.
package autofix

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
	"github.com/minya/logger"
	"github.com/minya/tgtorrentbot/internal/category"
	"github.com/minya/tgtorrentbot/internal/mediatools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// DefaultModel is the Claude model used when none is configured.
const DefaultModel = "claude-opus-5-5"

// allowedTools are the tools Claude may call. remove_torrent (which can
// delete data) and abs_remove_item are left out.
var allowedTools = []string{
	"abs_remove_issues",
	"abs_rescan",
	"jellyfin_rescan",
	"list_media",
	"move_media",
	"read_tags",
	"write_tags",
}

// Categories are the categories the fixer looks at, written with the
// constants of internal/category so a renamed or removed one breaks the
// build. Every other category must be in skippedCategories
// (TestEveryCategoryDecided), so a new one forces a decision.
var Categories = []category.Category{
	category.Audiobooks,
	category.Movies,
	category.Music,
	category.MusicVideos,
	category.Shows,
}

// skippedCategories are left alone: "others" holds anything.
var skippedCategories = []category.Category{category.Others}

const (
	maxTurns   = 30
	runTimeout = 10 * time.Minute
	maxTokens  = 16000
	// maxListed caps the file listing in the first message.
	maxListed = 200
)

const systemPrompt = `You maintain a home media library. A download has just completed. Your job is to make it show up correctly in Jellyfin (movies, shows, music, music videos) and Audiobookshelf (audiobooks), using the tools you have. Nobody is watching: act on your own, then report. This run is one-shot: nobody can answer your questions, and you will not see a reply to your report.

Work only on the item named in the message. Tool calls on other paths or other torrents are refused.

Inspect before you change anything. Most downloads are fine as they are; when nothing is wrong, change nothing and say so. Every change must have a concrete reason you saw in the data.

What to look for, by category:
- audiobooks, music, musicvideos: call read_tags (recursive=true on a directory). When files report encoding_hint "cp1251", call write_tags with fix_encoding="cp1251" on the item. Only override individual tags when they are clearly wrong or missing (e.g. no track numbers on a multi-file audiobook where the file names give the order); do not rewrite tags that are merely stylistically different.
- shows: Jellyfin matches shows by folder name. If the folder name is release junk (quality, codec, release group, dots for spaces) or a transliterated/Russian title, rename it with move_media to "Canonical English Title (Year)", e.g. "Proslushka.S01.2002.WEB-DL.1080p" -> "The Wire (2002)". Keep it in the same category directory. If you are not confident which show it is, leave it.
- movies: the same for a movie folder or single movie file: "Title (Year)" (keep a single file's extension). If unsure, leave it.

Changing the files stops seeding: write_tags and move_media on a torrent's data need its torrent_id, and they remove the torrent from Transmission (files kept). That is acceptable when a fix is needed, and a reason not to make cosmetic changes.

After changes, refresh the media server: abs_rescan after audiobook changes; jellyfin_rescan after changes to shows, movies, music or music videos. After moving an audiobook, call abs_rescan and then abs_remove_issues.

Finish with a short plain-text report for the owner (a Telegram message, no Markdown): what you changed and why, or "No changes needed" with a few words on why. At most 6 lines.

When you found something wrong and left it because only the owner can decide (for example, two shows share the title and you cannot tell which this is), start the report with the line "NEEDS INPUT", then say what is wrong and give each candidate with the exact change it means (e.g. the folder name for each), so the owner can apply one through Claude's tgtorrentbot connector or by hand. Never offer to do it later: you will not see an answer.`

// needsInputMarker starts a report on a problem left for the owner.
const needsInputMarker = "NEEDS INPUT"

// Item is the completed torrent a run works on.
type Item struct {
	TorrentID int
	Name      string
	Category  string
	// ChatID and ReplyTo are where the report goes: the chat that started
	// the download and its "Completed" message (0: no reply).
	ChatID  int64
	ReplyTo int64
	// Notice is the text of the "Completed" message, which the fixer's
	// status is appended to.
	Notice string
	// Root is the resolved path of the item's data; updated when Claude
	// renames the item itself.
	Root string
}

// Result is what a run did.
type Result struct {
	Summary string
	// NeedsInput is set when Claude left a problem for the owner to
	// decide (its report started with needsInputMarker, removed from
	// Summary).
	NeedsInput bool
	// Changes lists the tool calls that changed something on disk.
	Changes []string
	Turns   int
}

// toolCaller is the part of *mcp.ClientSession a run needs.
type toolCaller interface {
	CallTool(ctx context.Context, params *mcp.CallToolParams) (*mcp.CallToolResult, error)
}

// Agent runs Claude over one item at a time.
type Agent struct {
	tools   *mediatools.Tools
	client  anthropic.Client
	model   string
	defs    []anthropic.BetaToolUnionParam
	session *mcp.ClientSession
}

// NewAgent connects to tools in process and returns an agent that calls
// Claude with apiKey. ctx bounds only the setup. opts are extra client
// options (tests point the client at a fake API).
func NewAgent(ctx context.Context, tools *mediatools.Tools, apiKey, model string, opts ...option.RequestOption) (*Agent, error) {
	// The session must outlive ctx: a run in progress at shutdown keeps
	// calling tools until the drain deadline.
	session, err := tools.Connect(context.WithoutCancel(ctx))
	if err != nil {
		return nil, err
	}
	list, err := session.ListTools(ctx, nil)
	if err != nil {
		session.Close()
		return nil, fmt.Errorf("listing tools: %w", err)
	}
	defs, err := claudeTools(list.Tools)
	if err != nil {
		session.Close()
		return nil, err
	}
	if model == "" {
		model = DefaultModel
	}
	return &Agent{
		tools:   tools,
		client:  anthropic.NewClient(append([]option.RequestOption{option.WithAPIKey(apiKey)}, opts...)...),
		model:   model,
		defs:    defs,
		session: session,
	}, nil
}

// Model is the Claude model the agent uses.
func (a *Agent) Model() string { return a.model }

// Close ends the tool session.
func (a *Agent) Close() error { return a.session.Close() }

// claudeTools converts the allowed MCP tools to Claude tool definitions,
// with the descriptions and schemas the tools publish, sorted by name so
// the request prefix stays byte-identical across runs (prompt caching).
func claudeTools(mcpTools []*mcp.Tool) ([]anthropic.BetaToolUnionParam, error) {
	byName := map[string]*mcp.Tool{}
	for _, t := range mcpTools {
		byName[t.Name] = t
	}
	var out []anthropic.BetaToolUnionParam
	for _, name := range allowedTools {
		t, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("no %s tool", name)
		}
		raw, err := json.Marshal(t.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("%s schema: %w", name, err)
		}
		var schema map[string]any
		if err := json.Unmarshal(raw, &schema); err != nil {
			return nil, fmt.Errorf("%s schema: %w", name, err)
		}
		in := anthropic.BetaToolInputSchemaParam{ExtraFields: map[string]any{}}
		for k, v := range schema {
			switch k {
			case "type", "$schema":
			case "properties":
				in.Properties = v
			case "required":
				for _, r := range v.([]any) {
					in.Required = append(in.Required, r.(string))
				}
			default:
				in.ExtraFields[k] = v
			}
		}
		tool := anthropic.BetaToolParam{
			Name:        t.Name,
			Description: anthropic.String(t.Description),
			InputSchema: in,
		}
		out = append(out, anthropic.BetaToolUnionParam{OfTool: &tool})
	}
	return out, nil
}

// Run lets Claude inspect and fix item. It returns an error only when the
// run could not finish (API error, refusal, turn cap); the result then holds
// what was changed before that.
func (a *Agent) Run(ctx context.Context, item *Item) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()

	var res Result
	messages := []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(a.describe(item))),
	}
	for res.Turns = 1; res.Turns <= maxTurns; res.Turns++ {
		resp, err := a.client.Beta.Messages.New(ctx, anthropic.BetaMessageNewParams{
			Model:     a.model,
			MaxTokens: maxTokens,
			System: []anthropic.BetaTextBlockParam{{
				Text:         systemPrompt,
				CacheControl: anthropic.NewBetaCacheControlEphemeralParam(),
			}},
			Tools:        a.defs,
			Messages:     messages,
			OutputConfig: anthropic.BetaOutputConfigParam{Effort: anthropic.BetaOutputConfigEffortMedium},
			// A safety-classifier decline is re-served by another model in
			// the same call instead of ending the run.
			Fallbacks: anthropic.BetaFallbacksParamOfDefault(),
			Betas:     []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01},
		})
		if err != nil {
			return res, fmt.Errorf("Claude API: %w", err)
		}
		messages = append(messages, resp.ToParam())

		switch resp.StopReason {
		case anthropic.BetaStopReasonToolUse:
		case anthropic.BetaStopReasonRefusal:
			return res, fmt.Errorf("Claude declined: %s", resp.StopDetails.Explanation)
		case anthropic.BetaStopReasonMaxTokens:
			return res, errors.New("Claude's reply hit max_tokens")
		default:
			res.Summary, res.NeedsInput = splitNeedsInput(strings.TrimSpace(replyText(resp)))
			return res, nil
		}

		var results []anthropic.BetaContentBlockParamUnion
		for _, block := range resp.Content {
			use, ok := block.AsAny().(anthropic.BetaToolUseBlock)
			if !ok {
				continue
			}
			text, isErr, change := a.callTool(ctx, item, use.Name, []byte(use.JSON.Input.Raw()))
			if change != "" {
				res.Changes = append(res.Changes, change)
			}
			results = append(results, anthropic.NewBetaToolResultBlock(use.ID, text, isErr))
		}
		messages = append(messages, anthropic.NewBetaUserMessage(results...))
	}
	res.Turns = maxTurns
	return res, fmt.Errorf("stopped after %d turns without finishing", maxTurns)
}

func replyText(resp *anthropic.BetaMessage) string {
	var b strings.Builder
	for _, block := range resp.Content {
		if t, ok := block.AsAny().(anthropic.BetaTextBlock); ok {
			b.WriteString(t.Text)
		}
	}
	return b.String()
}

// describe is the first user message: the facts about the item.
func (a *Agent) describe(item *Item) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Completed download:\n")
	fmt.Fprintf(&b, "- name: %s\n", item.Name)
	fmt.Fprintf(&b, "- category: %s\n", item.Category)
	fmt.Fprintf(&b, "- path: %s\n", a.tools.Display(item.Root))
	fmt.Fprintf(&b, "- torrent_id: %d\n", item.TorrentID)
	files, truncated := a.tools.Files(item.Root, maxListed)
	fmt.Fprintf(&b, "Files (%d", len(files))
	if truncated {
		fmt.Fprintf(&b, ", truncated at %d", maxListed)
	}
	b.WriteString("):\n")
	for _, f := range files {
		fmt.Fprintf(&b, "  %s (%d bytes)\n", a.tools.Display(f.Path), f.Size)
	}
	return b.String()
}

// callTool checks a tool call against the run's scope, runs it, and returns
// the text for the tool_result, whether it is an error, and a description
// of the change it made ("" when it changed nothing).
func (a *Agent) callTool(ctx context.Context, item *Item, name string, input []byte) (text string, isErr bool, change string) {
	var args map[string]any
	if err := json.Unmarshal(input, &args); err != nil {
		return fmt.Sprintf("invalid tool input: %v", err), true, ""
	}
	if err := a.scopeGuard(item, name, args); err != nil {
		logger.Warn("[Autofix] %s refused for %q: %v", name, item.Name, err)
		return "refused: " + err.Error(), true, ""
	}
	res, err := a.session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return fmt.Sprintf("tool call failed: %v", err), true, ""
	}
	var parts []string
	for _, c := range res.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, t.Text)
		}
	}
	text = strings.Join(parts, "\n")
	logger.Info("[Autofix] %s %s -> error=%v", name, string(input), res.IsError)
	if res.IsError {
		return text, true, ""
	}
	return text, false, a.recordChange(item, name, args, res)
}

// recordChange describes what a successful mutating call changed, and
// follows a rename of the item itself, so later calls on the new path are
// in scope.
func (a *Agent) recordChange(item *Item, name string, args map[string]any, res *mcp.CallToolResult) string {
	var out struct {
		Moved   bool `json:"moved"`
		Changed int  `json:"changed"`
	}
	if raw, err := json.Marshal(res.StructuredContent); err == nil {
		_ = json.Unmarshal(raw, &out)
	}
	switch name {
	case "move_media":
		if !out.Moved {
			return ""
		}
		src, _ := args["source"].(string)
		dst, _ := args["destination"].(string)
		// The item itself was renamed: follow it.
		if _, err := os.Lstat(item.Root); errors.Is(err, fs.ErrNotExist) {
			if dstAbs, err := a.tools.Resolve(dst); err == nil {
				item.Root = dstAbs
			}
		}
		return fmt.Sprintf("moved %s → %s", src, dst)
	case "write_tags":
		if out.Changed == 0 {
			return ""
		}
		p, _ := args["path"].(string)
		return fmt.Sprintf("rewrote tags of %d file(s) in %s", out.Changed, p)
	}
	return ""
}

// scopeGuard confines a call to item: paths must be the item or inside it,
// a destination must stay in the item's category directory, and a
// torrent_id must be the item's.
func (a *Agent) scopeGuard(item *Item, name string, args map[string]any) error {
	if !slices.Contains(allowedTools, name) {
		return fmt.Errorf("tool %s is not available here", name)
	}
	for _, key := range []string{"path", "source"} {
		v, ok := args[key]
		if !ok {
			continue
		}
		p, _ := v.(string)
		abs, err := a.tools.Resolve(p)
		if err != nil {
			return fmt.Errorf("%s: %v", key, err)
		}
		if abs != item.Root && !mediatools.IsWithin(abs, item.Root) {
			return fmt.Errorf("%s %q is outside the item being fixed (%s)", key, p, a.tools.Display(item.Root))
		}
	}
	if v, ok := args["destination"]; ok {
		p, _ := v.(string)
		abs, err := a.tools.ResolveDest(p)
		if err != nil {
			return fmt.Errorf("destination: %v", err)
		}
		if !mediatools.IsWithin(abs, a.tools.CategoryDir(item.Category)) {
			return fmt.Errorf("destination %q must stay in %s/", p, item.Category)
		}
	}
	if v, ok := args["torrent_id"]; ok && v != nil {
		id, ok := v.(float64)
		if !ok || int(id) != item.TorrentID {
			return fmt.Errorf("torrent_id must be %d, the item's own torrent", item.TorrentID)
		}
	}
	return nil
}

// splitNeedsInput removes needsInputMarker (any case, with an optional
// colon) from the start of summary and reports whether it was there.
func splitNeedsInput(summary string) (string, bool) {
	if len(summary) < len(needsInputMarker) || !strings.EqualFold(summary[:len(needsInputMarker)], needsInputMarker) {
		return summary, false
	}
	rest := strings.TrimLeft(summary[len(needsInputMarker):], ": \t")
	return strings.TrimSpace(rest), true
}
