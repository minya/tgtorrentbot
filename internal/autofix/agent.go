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
	"path/filepath"
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

// mutatingTools change files; a call to one is never cut off midway.
var mutatingTools = map[string]bool{"move_media": true, "write_tags": true}

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
- audiobooks, music: read_tags and write_tags handle only .mp3 files; when the listed files include none (e.g. a FLAC release), there are no tags to fix. Otherwise call read_tags (recursive=true on a directory). When files report encoding_hint "cp1251", call write_tags with fix_encoding="cp1251" on the item. Only override individual tags when they are clearly wrong or missing (e.g. no track numbers on a multi-file audiobook where the file names give the order); do not rewrite tags that are merely stylistically different.
- shows: Jellyfin matches shows by folder name. If the folder name is release junk (quality, codec, release group, dots for spaces) or a transliterated/Russian title, rename it with move_media to "Canonical English Title (Year)", e.g. "Proslushka.S01.2002.WEB-DL.1080p" -> "The Wire (2002)". Keep it in the same category directory. Before renaming, check with list_media(category="shows", query=...) whether the show already has a folder (an earlier season): if it does, move the whole download into it as "Season NN" instead, e.g. "ShowA - Season 02" -> "ShowA (2019)/Season 02". If you are not confident which show it is, leave it.
- movies: the same for a movie folder or single movie file: "Title (Year)" (keep a single file's extension). If unsure, leave it.
- musicvideos: these are video files, so the tag tools do not apply. Jellyfin matches music videos by file name: if a file or folder name is release junk or transliterated, rename it with move_media to "Artist - Title" (keep a file's extension). If unsure, leave it.

Changing the files stops seeding: write_tags and move_media on a torrent's data need its torrent_id, and they remove the torrent from Transmission (files kept). That is acceptable when a fix is needed, and a reason not to make cosmetic changes. If such a call is refused because another torrent also tracks the data (e.g. the same release added twice), stop there: you cannot remove other torrents, so say in the report which torrents track it, and the owner can remove the duplicate.

After changes, refresh the media server: abs_rescan after audiobook changes; jellyfin_rescan after changes to shows, movies, music or music videos. A rescan tool missing from your tools means that server is not set up here: skip it. After moving an audiobook, call abs_rescan and then abs_remove_issues.

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
	tools *mediatools.Tools
	// allowed are the allowedTools that can work here: a media server's
	// tools are left out when it is not configured.
	allowed []string
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
	allowed := usableTools(tools)
	defs, err := claudeTools(list.Tools, allowed)
	if err != nil {
		session.Close()
		return nil, err
	}
	if model == "" {
		model = DefaultModel
	}
	return &Agent{
		allowed: allowed,
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

// usableTools is allowedTools without the tools of a media server that is
// not configured, which would only return an error.
func usableTools(tools *mediatools.Tools) []string {
	var out []string
	for _, name := range allowedTools {
		switch {
		case name == "jellyfin_rescan" && !tools.HasJellyfin():
		case strings.HasPrefix(name, "abs_") && !tools.HasAudiobookshelf():
		default:
			out = append(out, name)
		}
	}
	return out
}

// claudeTools converts the allowed MCP tools to Claude tool definitions,
// with the descriptions and schemas the tools publish, sorted by name so
// the request prefix stays byte-identical across runs (prompt caching).
func claudeTools(mcpTools []*mcp.Tool, allowed []string) ([]anthropic.BetaToolUnionParam, error) {
	byName := map[string]*mcp.Tool{}
	for _, t := range mcpTools {
		byName[t.Name] = t
	}
	var out []anthropic.BetaToolUnionParam
	for _, name := range allowed {
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
	// The mutating tools stop at a safe point when the run ends (timeout,
	// shutdown); callTool waits for them to get there.
	a.tools.SetStop(ctx)
	defer a.tools.SetStop(context.Background())

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
			Tools:    a.defs,
			Messages: messages,
			// Automatic caching: the breakpoint moves to the end of the
			// conversation on every turn, so earlier turns (large tag
			// listings included) are re-read from the cache, not re-billed
			// in full.
			CacheControl: anthropic.NewBetaCacheControlEphemeralParam(),
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
		logger.Info("[Autofix] turn %d: input %d, cache read %d, cache write %d, output %d tokens", res.Turns,
			resp.Usage.InputTokens, resp.Usage.CacheReadInputTokens, resp.Usage.CacheCreationInputTokens, resp.Usage.OutputTokens)

		switch resp.StopReason {
		case anthropic.BetaStopReasonToolUse:
		case anthropic.BetaStopReasonPauseTurn:
			// The server paused a long turn: send the conversation back
			// as it is (the assistant turn is appended) to let it go on.
			continue
		case anthropic.BetaStopReasonEndTurn, anthropic.BetaStopReasonStopSequence:
			res.Summary, res.NeedsInput = splitNeedsInput(strings.TrimSpace(replyText(resp)))
			return res, nil
		case anthropic.BetaStopReasonRefusal:
			return res, fmt.Errorf("Claude declined: %s", resp.StopDetails.Explanation)
		case anthropic.BetaStopReasonMaxTokens:
			return res, errors.New("Claude's reply hit max_tokens")
		default:
			// model_context_window_exceeded, compaction, or anything new:
			// the run did not finish, so it must not pass for "no changes".
			return res, fmt.Errorf("Claude stopped with %q before finishing", resp.StopReason)
		}

		var results []anthropic.BetaContentBlockParamUnion
		for _, block := range resp.Content {
			use, ok := block.AsAny().(anthropic.BetaToolUseBlock)
			if !ok {
				continue
			}
			if ctx.Err() != nil {
				// No new call once the run is over; the next API call
				// returns the error.
				results = append(results, anthropic.NewBetaToolResultBlock(use.ID, "not run: the run was stopped", true))
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
	callCtx := ctx
	if mutatingTools[name] {
		// Not cut off by the end of the run: the tool stops at its own
		// safe point (SetStop) and returns what it changed, which is then
		// reported. The bound is only a backstop.
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), runTimeout)
		defer cancel()
	}
	res, err := a.session.CallTool(callCtx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		if mutatingTools[name] {
			return fmt.Sprintf("tool call failed: %v", err), true,
				fmt.Sprintf("outcome unknown: %s %s (the torrent may have been removed from Transmission)", name, string(input))
		}
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
		Moved   bool       `json:"moved"`
		Changed int        `json:"changed"`
		Files   []struct{} `json:"files"`
		Total   int        `json:"total"`
		Stopped bool       `json:"stopped"`
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
		if out.Stopped {
			return fmt.Sprintf("rewrote tags of %d file(s) in %s, then stopped after %d of %d files; the rest are unchanged",
				out.Changed, p, len(out.Files), out.Total)
		}
		return fmt.Sprintf("rewrote tags of %d file(s) in %s", out.Changed, p)
	}
	return ""
}

// scopeGuard confines a call to item: paths must be the item or inside it,
// a destination must have one of the shapes destinationGuard allows, and a
// torrent_id must be the item's.
func (a *Agent) scopeGuard(item *Item, name string, args map[string]any) error {
	if !slices.Contains(a.allowed, name) {
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
		if err := a.destinationGuard(item, args, v); err != nil {
			return err
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

// destinationGuard allows a move of the item only in three shapes, so a
// run cannot put its files into another item's folder or pull a part of
// the item out of it:
//
//  1. the item itself is renamed: the source is the item, and the
//     destination is directly in its category directory (a single-file
//     item may also go into a new folder there: movies/T (Y)/T (Y).mkv);
//  2. the item is reorganised: source and destination are inside the item;
//  3. the whole item (a folder) moves into an existing folder of its
//     category as a new subfolder: shows/A - Season 02 -> shows/A (2019)/Season 02.
//
// move_media itself refuses an existing destination, so nothing is
// overwritten or merged file by file.
func (a *Agent) destinationGuard(item *Item, args map[string]any, v any) error {
	p, _ := v.(string)
	dst, err := a.tools.ResolveDest(p)
	if err != nil {
		return fmt.Errorf("destination: %v", err)
	}
	srcArg, _ := args["source"].(string)
	src, err := a.tools.Resolve(srcArg)
	if err != nil {
		return fmt.Errorf("source: %v", err)
	}
	if src != item.Root {
		// Shape 2. The source was already checked to be inside the item.
		if !mediatools.IsWithin(dst, item.Root) {
			return fmt.Errorf("destination %q: a part of the item may only move within the item (%s)", p, a.tools.Display(item.Root))
		}
		return nil
	}
	catDir := a.tools.CategoryDir(item.Category)
	parent := filepath.Dir(dst)
	if parent == catDir {
		return nil // shape 1
	}
	if filepath.Dir(parent) == catDir {
		info, err := os.Stat(item.Root)
		isDir := err == nil && info.IsDir()
		_, perr := os.Lstat(parent)
		parentExists := perr == nil
		switch {
		case isDir && parentExists:
			return nil // shape 3
		case !isDir && !parentExists:
			return nil // shape 1, a single file into its own new folder
		}
	}
	return fmt.Errorf("destination %q: the item may only be renamed in %s/ (%s/New Name), or moved as a whole into an existing folder there (%s/Existing/New Name)",
		p, item.Category, item.Category, item.Category)
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
