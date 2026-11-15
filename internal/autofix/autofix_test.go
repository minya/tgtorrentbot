package autofix

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/anthropics/anthropic-sdk-go/v2/option"
	"github.com/minya/tgtorrentbot/internal/category"
	"github.com/minya/tgtorrentbot/internal/mediatools"
	"github.com/odwrtw/transmission"
)

// fakeClaude serves scripted Messages API replies in order and records the
// request bodies.
type fakeClaude struct {
	mu       sync.Mutex
	replies  []string // JSON message bodies; the last one repeats
	requests []map[string]any
}

func (f *fakeClaude) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req map[string]any
	_ = json.Unmarshal(body, &req)
	f.mu.Lock()
	f.requests = append(f.requests, req)
	reply := f.replies[min(len(f.requests), len(f.replies))-1]
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, reply)
}

func toolUseReply(name string, input any) string {
	in, _ := json.Marshal(input)
	return fmt.Sprintf(`{"id":"msg","type":"message","role":"assistant","model":"m",
		"content":[{"type":"tool_use","id":"tu_%s","name":%q,"input":%s}],
		"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1}}`, name, name, in)
}

func textReply(text string) string {
	return fmt.Sprintf(`{"id":"msg","type":"message","role":"assistant","model":"m",
		"content":[{"type":"text","text":%q}],
		"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`, text)
}

const refusalReply = `{"id":"msg","type":"message","role":"assistant","model":"m","content":[],
	"stop_reason":"refusal","stop_details":{"type":"refusal","category":null,"explanation":"no"},
	"usage":{"input_tokens":1,"output_tokens":1}}`

type fakeTorrents struct {
	torrents []*transmission.Torrent
	removed  []int
	keptData bool
}

func (f *fakeTorrents) GetTorrents() ([]*transmission.Torrent, error) { return f.torrents, nil }

func (f *fakeTorrents) RemoveTorrents(ts []*transmission.Torrent, removeData bool) error {
	f.keptData = !removeData
	for _, t := range ts {
		f.removed = append(f.removed, t.ID)
	}
	return nil
}

// showTorrent is torrent 7, whose data is shows/Proslushka.S01.2002.
func showTorrent(root string) *transmission.Torrent {
	return &transmission.Torrent{ID: 7, Name: "Proslushka.S01.2002", DownloadDir: filepath.Join(root, "shows"), PercentDone: 1}
}

// newAutofixTest returns an agent over the media tools on a temp download
// dir holding shows/Proslushka.S01.2002 (torrent 7) and shows/Other,
// talking to a fake Claude.
func newAutofixTest(t *testing.T, replies ...string) (*Agent, *Item, *fakeClaude, *fakeTorrents, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(root, "shows", "Proslushka.S01.2002")
	for _, d := range []string{src, filepath.Join(root, "shows", "Other")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(src, "ep.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := &fakeTorrents{torrents: []*transmission.Torrent{showTorrent(root)}}
	// Jellyfin is configured (never called), Audiobookshelf is not.
	tools := mediatools.New(mediatools.Config{DownloadPath: root, IncompletePath: filepath.Join(root, "incomplete"),
		JellyfinURL: "http://127.0.0.1:1", JellyfinAPIKey: "k"}, fake)

	claude := &fakeClaude{replies: replies}
	api := httptest.NewServer(claude)
	t.Cleanup(api.Close)

	agent, err := NewAgent(context.Background(), tools, "k", "", option.WithBaseURL(api.URL), option.WithMaxRetries(0))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { agent.close() })
	item := &Item{TorrentID: 7, Name: "Proslushka.S01.2002", Category: "shows", Root: src}
	return agent, item, claude, fake, root
}

func TestAutofixRenamesShow(t *testing.T) {
	agent, item, claude, fake, root := newAutofixTest(t,
		toolUseReply("move_media", map[string]any{
			"source": "shows/Proslushka.S01.2002", "destination": "shows/The Wire (2002)", "torrent_id": 7,
		}),
		toolUseReply("read_tags", map[string]any{"path": "shows/The Wire (2002)"}),
		textReply("Renamed to The Wire (2002)."),
	)
	res, err := agent.Run(context.Background(), item)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "shows", "The Wire (2002)", "ep.mkv")); err != nil {
		t.Fatalf("not renamed: %v", err)
	}
	if len(fake.removed) != 1 || fake.removed[0] != 7 || !fake.keptData {
		t.Fatalf("torrent 7 not untracked with data kept: %+v", fake)
	}
	if res.Summary != "Renamed to The Wire (2002)." || len(res.Changes) != 1 || res.Turns != 3 {
		t.Fatalf("result %+v", res)
	}
	// The run follows the rename: read_tags on the new path was in scope.
	if item.Root != filepath.Join(root, "shows", "The Wire (2002)") {
		t.Fatalf("root not followed: %s", item.Root)
	}
	if got := lastToolResult(t, claude.requests[2]); strings.Contains(fmt.Sprint(got["content"]), "refused") {
		t.Fatalf("read_tags after the rename was refused: %v", got)
	}

	first := claude.requests[0]
	if first["model"] != DefaultModel {
		t.Fatalf("model %v", first["model"])
	}
	var names []string
	for _, tool := range first["tools"].([]any) {
		names = append(names, tool.(map[string]any)["name"].(string))
	}
	// No abs_* tools without Audiobookshelf.
	if want := "jellyfin_rescan,list_media,move_media,read_tags,write_tags"; strings.Join(names, ",") != want {
		t.Fatalf("tools %v, want %v", names, want)
	}
	if !strings.Contains(fmt.Sprint(first["messages"]), "torrent_id: 7") {
		t.Fatal("first message does not describe the item")
	}
}

// lastToolResult returns the last tool_result block of a request.
func lastToolResult(t *testing.T, req map[string]any) map[string]any {
	t.Helper()
	msgs := req["messages"].([]any)
	content := msgs[len(msgs)-1].(map[string]any)["content"].([]any)
	return content[len(content)-1].(map[string]any)
}

func TestAutofixScopeGuard(t *testing.T) {
	agent, item, _, _, _ := newAutofixTest(t)
	cases := []struct {
		tool string
		args map[string]any
		ok   bool
	}{
		{"read_tags", map[string]any{"path": "shows/Proslushka.S01.2002"}, true},
		{"read_tags", map[string]any{"path": "shows/Proslushka.S01.2002/ep.mkv"}, true},
		{"read_tags", map[string]any{"path": "shows/Other"}, false},
		{"read_tags", map[string]any{"path": "shows"}, false},
		{"move_media", map[string]any{"source": "shows/Proslushka.S01.2002", "destination": "shows/The Wire (2002)"}, true},
		{"move_media", map[string]any{"source": "shows/Proslushka.S01.2002", "destination": "movies/The Wire (2002)"}, false},
		{"move_media", map[string]any{"source": "shows/Other", "destination": "shows/X"}, false},
		// The three allowed shapes, and the moves out of the item.
		{"move_media", map[string]any{"source": "shows/Proslushka.S01.2002", "destination": "shows/Other/Season 01"}, true},
		{"move_media", map[string]any{"source": "shows/Proslushka.S01.2002/ep.mkv", "destination": "shows/Proslushka.S01.2002/Season 01/ep.mkv"}, true},
		{"move_media", map[string]any{"source": "shows/Proslushka.S01.2002/ep.mkv", "destination": "shows/Other/S01E99.mkv"}, false},
		{"move_media", map[string]any{"source": "shows/Proslushka.S01.2002/ep.mkv", "destination": "shows/Other/ep.mkv"}, false},
		{"move_media", map[string]any{"source": "shows/Proslushka.S01.2002/ep.mkv", "destination": "shows/ep.mkv"}, false},
		{"move_media", map[string]any{"source": "shows/Proslushka.S01.2002", "destination": "shows/New/Season 01"}, false},
		{"move_media", map[string]any{"source": "shows/Proslushka.S01.2002", "destination": "shows/Other/Season 01/Extra"}, false},
		{"write_tags", map[string]any{"path": "shows/Proslushka.S01.2002", "torrent_id": float64(7)}, true},
		{"write_tags", map[string]any{"path": "shows/Proslushka.S01.2002", "torrent_id": float64(8)}, false},
		{"remove_torrent", map[string]any{"torrent_id": float64(7)}, false},
		{"abs_remove_item", map[string]any{"item_id": "li_1"}, false},
		{"jellyfin_rescan", map[string]any{}, true},
		{"abs_rescan", map[string]any{}, false}, // Audiobookshelf not configured
	}
	for _, c := range cases {
		err := agent.scopeGuard(item, c.tool, c.args)
		if (err == nil) != c.ok {
			t.Errorf("%s %v: err=%v, want ok=%v", c.tool, c.args, err, c.ok)
		}
	}
}

// A single-file item may be renamed into a new folder of its own, but
// not put into another item's folder.
func TestAutofixScopeGuardSingleFile(t *testing.T) {
	agent, _, _, _, root := newAutofixTest(t)
	movie := filepath.Join(root, "movies", "Film.2001.mkv")
	for _, d := range []string{filepath.Dir(movie), filepath.Join(root, "movies", "Other (1999)")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(movie, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	item := &Item{TorrentID: 7, Category: "movies", Root: movie}
	for dst, ok := range map[string]bool{
		"movies/Film (2001).mkv":              true,
		"movies/Film (2001)/Film (2001).mkv":  true,
		"movies/Other (1999)/Film (2001).mkv": false,
	} {
		err := agent.scopeGuard(item, "move_media", map[string]any{"source": "movies/Film.2001.mkv", "destination": dst})
		if (err == nil) != ok {
			t.Errorf("%s: err=%v, want ok=%v", dst, err, ok)
		}
	}
}

func TestAutofixRefusedCallIsToolError(t *testing.T) {
	agent, item, claude, _, root := newAutofixTest(t,
		toolUseReply("move_media", map[string]any{"source": "shows/Other", "destination": "shows/Y"}),
		textReply("No changes needed."),
	)
	res, err := agent.Run(context.Background(), item)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "shows", "Other")); err != nil {
		t.Fatal("other item was moved")
	}
	if got := lastToolResult(t, claude.requests[1]); got["is_error"] != true {
		t.Fatalf("refusal not reported as a tool error: %v", got)
	}
	if len(res.Changes) != 0 {
		t.Fatalf("changes %v", res.Changes)
	}
}

func TestAutofixTurnCap(t *testing.T) {
	agent, item, claude, _, _ := newAutofixTest(t,
		toolUseReply("read_tags", map[string]any{"path": "shows/Proslushka.S01.2002"}))
	_, err := agent.Run(context.Background(), item)
	if err == nil || !strings.Contains(err.Error(), "turns") {
		t.Fatalf("err %v, want the turn cap", err)
	}
	if len(claude.requests) != maxTurns {
		t.Fatalf("%d requests, want %d", len(claude.requests), maxTurns)
	}
}

func TestAutofixRefusal(t *testing.T) {
	agent, item, _, _, _ := newAutofixTest(t, refusalReply)
	if _, err := agent.Run(context.Background(), item); err == nil || !strings.Contains(err.Error(), "declined") {
		t.Fatalf("err %v", err)
	}
}

// recordingNotifier records replies and status marks.
type recordingNotifier struct {
	replies  []string
	marks    []string
	replyErr error
}

func (n *recordingNotifier) Reply(_ context.Context, _ *Item, text string) error {
	n.replies = append(n.replies, text)
	return n.replyErr
}

func (n *recordingNotifier) Mark(_ context.Context, _ *Item, status string) error {
	n.marks = append(n.marks, status)
	return nil
}

// Every run marks the notice with its outcome; only a change, a
// problem left for the owner or a failure also gets a reply.
func TestAutofixOutcomeReportedOnNotice(t *testing.T) {
	for _, c := range []struct {
		name      string
		replies   []string
		replyErr  error
		wantReply string // prefix; "" for no reply
		wantMark  string
	}{
		{"no changes", []string{textReply("No changes needed.")}, nil, "", StatusClean},
		{"rename", []string{
			toolUseReply("move_media", map[string]any{"source": "shows/Proslushka.S01.2002", "destination": "shows/The Wire (2002)", "torrent_id": 7}),
			textReply("Renamed."),
		}, nil, "Auto-fix: Proslushka.S01.2002", StatusFixed},
		{"needs input", []string{textReply("NEEDS INPUT\nTwo shows are called Dark Matter: rename to \"Dark Matter (2015)\" or \"Dark Matter (2024)\".")},
			nil, "Auto-fix needs your decision: Proslushka.S01.2002\n\nTwo shows", StatusNeedsInput},
		{"failure", []string{refusalReply}, nil, "Auto-fix: Proslushka.S01.2002\nStopped:", StatusFailed},
		{"reply lost", []string{refusalReply}, errors.New("telegram down"), "Auto-fix: Proslushka.S01.2002", StatusFailed + statusUndelivered},
	} {
		t.Run(c.name, func(t *testing.T) {
			agent, item, _, _, _ := newAutofixTest(t, c.replies...)
			n := &recordingNotifier{replyErr: c.replyErr}
			NewFixer(agent, n).handle(context.Background(), item)
			if c.wantReply == "" && len(n.replies) != 0 {
				t.Fatalf("replied %q, want none", n.replies)
			}
			if c.wantReply != "" && (len(n.replies) != 1 || !strings.HasPrefix(n.replies[0], c.wantReply)) {
				t.Fatalf("replied %q, want one starting %q", n.replies, c.wantReply)
			}
			if want := []string{StatusChecking, c.wantMark}; !slices.Equal(n.marks, want) {
				t.Fatalf("marks %q, want %q", n.marks, want)
			}
		})
	}
}

func TestSplitNeedsInput(t *testing.T) {
	for in, want := range map[string]struct {
		summary string
		needs   bool
	}{
		"NEEDS INPUT\nPick one.": {"Pick one.", true},
		"Needs input: pick one.": {"pick one.", true},
		"No changes needed.":     {"No changes needed.", false},
		"NEEDS":                  {"NEEDS", false},
	} {
		if s, n := splitNeedsInput(in); s != want.summary || n != want.needs {
			t.Errorf("splitNeedsInput(%q) = %q, %v; want %q, %v", in, s, n, want.summary, want.needs)
		}
	}
}

func TestAutofixSubmit(t *testing.T) {
	agent, _, _, _, root := newAutofixTest(t)
	n := &recordingNotifier{}
	f := NewFixer(agent, n)
	show := showTorrent(root)
	show.Labels = []string{"123", "shows", "123"}
	other := &transmission.Torrent{ID: 9, Name: "Other", DownloadDir: filepath.Join(root, "shows"), Labels: []string{"1", "others"}}
	missing := &transmission.Torrent{ID: 10, Name: "Gone", DownloadDir: filepath.Join(root, "shows"), Labels: []string{"1", "shows"}}
	if !f.Submit(show, 123, 55, "Completed: x") || f.Submit(other, 1, 0, "") || f.Submit(missing, 1, 0, "") {
		t.Fatal("want only the show queued")
	}
	item := <-f.queue
	if item.TorrentID != 7 || item.ChatID != 123 || item.ReplyTo != 55 || item.Category != "shows" || item.Notice != "Completed: x" ||
		item.Root != filepath.Join(root, "shows", "Proslushka.S01.2002") {
		t.Fatalf("item %+v", item)
	}
	// others is not the fixer's: its notice is left alone. The worker is
	// idle, so the show is not marked "queued".
	if want := []string{StatusNoData}; !slices.Equal(n.marks, want) {
		t.Fatalf("marks %q, want %q", n.marks, want)
	}
}

func TestAutofixSubmitQueueFull(t *testing.T) {
	agent, _, _, _, root := newAutofixTest(t)
	n := &recordingNotifier{}
	f := NewFixer(agent, n)
	f.queue = make(chan *Item) // no room
	show := showTorrent(root)
	show.Labels = []string{"1", "shows"}
	if f.Submit(show, 1, 2, "Completed: x") {
		t.Fatal("queued with a full queue")
	}
	if want := []string{StatusBusy}; !slices.Equal(n.marks, want) {
		t.Fatalf("marks %q, want %q", n.marks, want)
	}
}

func TestAutofixReportTruncated(t *testing.T) {
	units := func(s string) int { return len(utf16.Encode([]rune(s))) }
	// A Cyrillic report over 4096 bytes but under 4096 characters
	// is not cut.
	fits := strings.Repeat("я", 3000)
	if text := report(&Item{Name: "x"}, Result{Summary: fits}, nil); !strings.Contains(text, fits) {
		t.Fatalf("a %d-character report was cut to %d characters", units(fits), units(text))
	}
	// One over the limit is cut to at most 4096 UTF-16 units, ending in "…".
	for _, long := range []string{strings.Repeat("я", 5000), strings.Repeat("😀", 3000)} {
		text := report(&Item{Name: "x"}, Result{Summary: long}, nil)
		if units(text) > telegramLimit || !strings.HasSuffix(text, "…") || !utf8.ValidString(text) {
			t.Fatalf("report is %d UTF-16 units, suffix ok %v, valid %v", units(text), strings.HasSuffix(text, "…"), utf8.ValidString(text))
		}
	}
}

// Every category is either handled or deliberately skipped, so a new
// category in internal/category forces a decision here.
func TestEveryCategoryDecided(t *testing.T) {
	for _, c := range category.All() {
		handled, skipped := slices.Contains(Categories, c), slices.Contains(skippedCategories, c)
		if handled == skipped {
			t.Errorf("category %s: handled %v, skipped %v; put it in exactly one of Categories and skippedCategories", c, handled, skipped)
		}
	}
	for _, c := range append(slices.Clone(Categories), skippedCategories...) {
		if !category.IsValid(string(c)) {
			t.Errorf("%s is not a category", c)
		}
	}
	// Every handled category has exactly one rule in the prompt, and a
	// skipped one has none.
	rules := map[category.Category]int{}
	for _, r := range categoryRules {
		for _, c := range r.cats {
			rules[c]++
		}
	}
	for _, c := range Categories {
		if rules[c] != 1 {
			t.Errorf("category %s is handled but has %d rules in the prompt; give it exactly one in categoryRules", c, rules[c])
		}
	}
	for _, c := range skippedCategories {
		if rules[c] != 0 {
			t.Errorf("category %s is skipped but has a rule in the prompt", c)
		}
	}
	for _, r := range categoryRules {
		if line := "- " + string(r.cats[0]); !strings.Contains(systemPrompt, line) {
			t.Errorf("rule for %s not rendered into the prompt", r.cats[0])
		}
	}
}

// When the run ends, a mutating call is not cut off: it stops at its
// own safe point (move_media before untracking) and its result comes back,
// instead of "tool call failed: context canceled" with the handler still
// running.
func TestAutofixStoppedRunWaitsForMutatingCall(t *testing.T) {
	agent, item, _, fake, root := newAutofixTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	agent.tools.SetStop(ctx)
	defer agent.tools.SetStop(context.Background())
	text, isErr, change := agent.callTool(ctx, item, "move_media",
		[]byte(`{"source":"shows/Proslushka.S01.2002","destination":"shows/The Wire (2002)","torrent_id":7}`))
	if !isErr || !strings.Contains(text, "nothing was moved") || change != "" {
		t.Fatalf("text %q isErr %v change %q", text, isErr, change)
	}
	if len(fake.removed) != 0 {
		t.Fatal("torrent untracked")
	}
	if _, err := os.Stat(filepath.Join(root, "shows", "Proslushka.S01.2002")); err != nil {
		t.Fatal("moved")
	}
}

// blockingRunner runs until its context ends; started is closed when a run
// begins.
type blockingRunner struct {
	started   chan struct{}
	cancelled atomic.Int64 // unix nanos when the run saw its ctx end
}

func (r *blockingRunner) Run(ctx context.Context, _ *Item) (Result, error) {
	close(r.started)
	<-ctx.Done()
	r.cancelled.Store(time.Now().UnixNano())
	return Result{}, ctx.Err()
}

// slowNotifier takes up to delay per call, giving up when ctx ends.
type slowNotifier struct {
	delay   time.Duration
	mu      sync.Mutex
	replies []string
	marks   []string
}

func (n *slowNotifier) wait(ctx context.Context) error {
	select {
	case <-time.After(n.delay):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (n *slowNotifier) Reply(ctx context.Context, _ *Item, text string) error {
	n.mu.Lock()
	n.replies = append(n.replies, text)
	n.mu.Unlock()
	return n.wait(ctx)
}

func (n *slowNotifier) Mark(ctx context.Context, _ *Item, status string) error {
	n.mu.Lock()
	n.marks = append(n.marks, status)
	n.mu.Unlock()
	return nil
}

// The drain stops the run reportBudget before its deadline, so the
// report goes out, and returns by the deadline even when Telegram is slow.
func TestFixerDrainKeepsDeadline(t *testing.T) {
	r := &blockingRunner{started: make(chan struct{})}
	n := &slowNotifier{delay: 3 * time.Second}
	f := newFixer(r, nil, n)
	f.reportBudget = 300 * time.Millisecond
	ctx, stop := context.WithCancel(context.Background())
	drain := f.Start(ctx)
	f.queue <- &Item{Name: "a"}
	<-r.started

	stop() // SIGTERM
	start := time.Now()
	deadline := start.Add(time.Second)
	drainCtx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	drain(drainCtx)
	took := time.Since(start)

	if took > 1100*time.Millisecond {
		t.Fatalf("drain took %v, past its 1 s deadline", took)
	}
	cancelledAt := time.Unix(0, r.cancelled.Load())
	if early := deadline.Sub(cancelledAt); early < 250*time.Millisecond {
		t.Fatalf("run stopped %v before the deadline, want about reportBudget (300ms)", early)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.replies) != 1 || !strings.Contains(n.replies[0], "Stopped: interrupted by a restart of the bot") {
		t.Fatalf("replies %q", n.replies)
	}
}

// A run cut short by its own time limit says so, not "deadline exceeded".
func TestStopReason(t *testing.T) {
	if got := stopReason(fmt.Errorf("Claude API: %w", context.Canceled)); got != "interrupted by a restart of the bot" {
		t.Errorf("canceled: %q", got)
	}
	if got := stopReason(fmt.Errorf("Claude API: %w", context.DeadlineExceeded)); !strings.HasPrefix(got, "took longer than") {
		t.Errorf("deadline: %q", got)
	}
}

// The agent's error for a cancelled run is recognised as a restart.
func TestAutofixCancelledRunIsRestart(t *testing.T) {
	agent, item, _, _, _ := newAutofixTest(t, textReply("x"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := agent.Run(ctx, item)
	if err == nil || stopReason(err) != "interrupted by a restart of the bot" {
		t.Fatalf("err %v -> %q", err, stopReason(err))
	}
}

// countingRunner counts runs.
type countingRunner struct{ runs atomic.Int32 }

func (r *countingRunner) Run(context.Context, *Item) (Result, error) {
	r.runs.Add(1)
	return Result{}, nil
}

// Once shutdown began, no queued item is run, even though the worker
// could see both the queue and the shutdown; the items left are marked
// skipped on their notices.
func TestFixerStartsNoRunAfterShutdown(t *testing.T) {
	for i := 0; i < 200; i++ {
		r := &countingRunner{}
		n := &recordingNotifier{}
		f := newFixer(r, nil, n)
		f.queue <- &Item{Name: "queued"}
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // shutdown before the worker looks
		drain := f.Start(ctx)
		drainCtx, cancelDrain := context.WithTimeout(context.Background(), time.Second)
		drain(drainCtx)
		cancelDrain()
		if r.runs.Load() != 0 {
			t.Fatalf("try %d: a run started after shutdown", i)
		}
		if len(n.marks) != 1 || n.marks[0] != StatusRestarted {
			t.Fatalf("try %d: marks %q, want %q", i, n.marks, StatusRestarted)
		}
	}
}

// Music videos are not sent to the .mp3-only tag tools, and the tag
// rule states that limit.
func TestPromptKeepsMusicVideosOffTagTools(t *testing.T) {
	for _, line := range strings.Split(systemPrompt, "\n") {
		if strings.Contains(line, "musicvideos") && strings.Contains(line, "call read_tags") {
			t.Errorf("prompt line sends music videos to read_tags: %q", line)
		}
		if strings.HasPrefix(line, "- audiobooks, music:") && !strings.Contains(line, "only .mp3") {
			t.Errorf("tag rule does not state the .mp3 limit: %q", line)
		}
	}
	if !strings.Contains(systemPrompt, "- musicvideos:") {
		t.Error("no rule for music videos")
	}
}

// A media server's tools are offered only when it is configured.
func TestUsableTools(t *testing.T) {
	for _, c := range []struct {
		cfg  mediatools.Config
		want string
	}{
		{mediatools.Config{}, "list_media,move_media,read_tags,write_tags"},
		{mediatools.Config{JellyfinURL: "u", JellyfinAPIKey: "k", AudiobookshelfURL: "u", AudiobookshelfAPIKey: "k"},
			strings.Join(allowedTools, ",")},
		{mediatools.Config{JellyfinURL: "u"}, "list_media,move_media,read_tags,write_tags"},
	} {
		if got := strings.Join(usableTools(mediatools.New(c.cfg, nil)), ","); got != c.want {
			t.Errorf("%+v: %s, want %s", c.cfg, got, c.want)
		}
	}
}

// Every request carries the top-level cache_control (automatic
// caching of the conversation) besides the system prompt's breakpoint.
func TestAutofixCachesConversation(t *testing.T) {
	agent, item, claude, _, _ := newAutofixTest(t,
		toolUseReply("read_tags", map[string]any{"path": "shows/Proslushka.S01.2002"}),
		textReply("No changes needed."),
	)
	if _, err := agent.Run(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	for i, req := range claude.requests {
		cc, _ := req["cache_control"].(map[string]any)
		if cc["type"] != "ephemeral" {
			t.Errorf("request %d: top-level cache_control %v", i, req["cache_control"])
		}
		sys := req["system"].([]any)[0].(map[string]any)
		if sys["cache_control"] == nil {
			t.Errorf("request %d: system breakpoint dropped", i)
		}
	}
}

// A torrent whose label and data disagree is skipped and marked,
// not queued for a fix that would move it to the label's category.
func TestAutofixSubmitSkipsLabelMismatch(t *testing.T) {
	agent, _, _, _, root := newAutofixTest(t)
	if err := os.MkdirAll(filepath.Join(root, "others", "Proslushka.S01"), 0o755); err != nil {
		t.Fatal(err)
	}
	n := &recordingNotifier{}
	f := NewFixer(agent, n)
	tr := &transmission.Torrent{ID: 8, Name: "Proslushka.S01", DownloadDir: filepath.Join(root, "others"), Labels: []string{"1", "shows", "1"}, PercentDone: 1}
	if f.Submit(tr, 1, 2, "Completed: x") {
		t.Fatal("queued a torrent labelled shows with its data in others/")
	}
	if want := []string{StatusMismatch}; !slices.Equal(n.marks, want) {
		t.Fatalf("marks %q, want %q", n.marks, want)
	}
}

// The prompt says what to do when another torrent tracks the data,
// since the tool errors point to remove_torrent, which the fixer lacks.
func TestPromptHandlesDuplicateTorrents(t *testing.T) {
	if slices.Contains(allowedTools, "remove_torrent") {
		t.Fatal("remove_torrent is allowed")
	}
	if !strings.Contains(systemPrompt, "another torrent also tracks the data") {
		t.Fatal("prompt has no rule for a duplicate torrent")
	}
}

func stopReply(reason string) string {
	return fmt.Sprintf(`{"id":"msg","type":"message","role":"assistant","model":"m",
		"content":[{"type":"text","text":"partial"}],
		"stop_reason":%q,"usage":{"input_tokens":1,"output_tokens":1}}`, reason)
}

// Only end_turn and stop_sequence finish a run; pause_turn continues
// it, and any other stop reason is an error, never "no changes".
func TestAutofixStopReasons(t *testing.T) {
	agent, item, claude, _, _ := newAutofixTest(t, stopReply("pause_turn"), textReply("No changes needed."))
	res, err := agent.Run(context.Background(), item)
	if err != nil || res.Summary != "No changes needed." || len(claude.requests) != 2 {
		t.Fatalf("pause_turn: res %+v err %v requests %d", res, err, len(claude.requests))
	}
	// The paused assistant turn is sent back as the last message.
	msgs := claude.requests[1]["messages"].([]any)
	if last := msgs[len(msgs)-1].(map[string]any); last["role"] != "assistant" {
		t.Fatalf("last message after pause_turn: %v", last)
	}
	for _, reason := range []string{"model_context_window_exceeded", "compaction", "something_new"} {
		agent, item, _, _, _ := newAutofixTest(t, stopReply(reason))
		if _, err := agent.Run(context.Background(), item); err == nil || !strings.Contains(err.Error(), reason) {
			t.Errorf("%s: err %v", reason, err)
		}
	}
	agent, item, _, _, _ = newAutofixTest(t, stopReply("stop_sequence"))
	if res, err := agent.Run(context.Background(), item); err != nil || res.Summary != "partial" {
		t.Errorf("stop_sequence: %+v %v", res, err)
	}
}

// The report names folders by their name, not the tool path, and the
// rule is worded so it cannot be read as a renaming rule.
func TestPromptReportNamesWithoutPaths(t *testing.T) {
	if !strings.Contains(systemPrompt, "In the report, refer to folders and files by their name alone") {
		t.Fatal("no naming rule for the report")
	}
}

// A download that completes once the drain has taken the queue is
// marked skipped at once, not queued for a run that will never come.
func TestFixerSubmitAfterShutdownIsSkipped(t *testing.T) {
	agent, _, _, _, root := newAutofixTest(t)
	n := &recordingNotifier{}
	f := NewFixer(agent, n)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	drain := f.Start(ctx)
	drainCtx, cancelDrain := context.WithTimeout(context.Background(), time.Second)
	defer cancelDrain()
	drain(drainCtx)

	show := showTorrent(root)
	show.Labels = []string{"1", "shows"}
	if f.Submit(show, 1, 2, "Completed: x") {
		t.Fatal("queued after the drain took the queue")
	}
	if want := []string{StatusRestarted}; !slices.Equal(n.marks, want) {
		t.Fatalf("marks %q, want %q", n.marks, want)
	}
}

// A drain that starts with its deadline already passed still takes
// the queue over (it used to leave it untouched in half the cases).
func TestFixerExpiredDrainTakesQueue(t *testing.T) {
	for i := 0; i < 200; i++ {
		f := newFixer(&countingRunner{}, nil, nil)
		f.queue <- &Item{Name: "queued"}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		drain := f.Start(ctx)
		expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		drain(expired)
		cancelExpired()
		if len(f.queue) != 0 || !f.isClosed() {
			t.Fatalf("try %d: queue %d, closed %v", i, len(f.queue), f.isClosed())
		}
	}
}

// "queued" is shown only when the download waits behind a run in
// progress or other queued downloads.
func TestAutofixSubmitMarksQueuedOnlyWhenWaiting(t *testing.T) {
	agent, _, _, _, root := newAutofixTest(t)
	show := showTorrent(root)
	show.Labels = []string{"1", "shows"}
	for _, c := range []struct {
		name    string
		running bool
		want    []string
	}{
		{"idle", false, nil},
		{"busy", true, []string{StatusQueued}},
	} {
		n := &recordingNotifier{}
		f := NewFixer(agent, n)
		f.running.Store(c.running)
		if !f.Submit(show, 1, 2, "Completed: x") {
			t.Fatalf("%s: not queued", c.name)
		}
		if !slices.Equal(n.marks, c.want) {
			t.Errorf("%s: marks %q, want %q", c.name, n.marks, c.want)
		}
	}
}
