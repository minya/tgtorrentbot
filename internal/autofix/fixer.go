package autofix

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/minya/logger"
	"github.com/minya/tgtorrentbot/internal/torrentlabels"
	"github.com/odwrtw/transmission"
)

// queueSize bounds the completions waiting for a run; more than that at
// once (a big batch finishing together) are dropped with a log line.
const queueSize = 64

// Notifier tells the item's chat what the fixer did with it.
type Notifier interface {
	// Reply sends a run's report as a reply to the item's notice.
	Reply(ctx context.Context, item *Item, text string) error
	// Mark shows status under the item's notice (an edit of it), so the
	// owner can tell "checked, fine" from "never looked at".
	Mark(ctx context.Context, item *Item, status string) error
}

// The statuses Mark shows. A run that changed something, needs the owner
// or failed also gets a reply; the others are the status alone.
const (
	StatusQueued      = "Auto-fix: queued"
	StatusChecking    = "Auto-fix: checking…"
	StatusClean       = "Auto-fix: checked, no changes needed"
	StatusFixed       = "Auto-fix: fixed, see the reply"
	StatusNeedsInput  = "Auto-fix: needs your decision, see the reply"
	StatusFailed      = "Auto-fix: stopped, see the reply"
	StatusNoData      = "Auto-fix: skipped, data not found"
	StatusBusy        = "Auto-fix: skipped, too many downloads at once"
	statusUndelivered = " (reply not delivered)"
)

// markTimeout bounds one status edit.
const markTimeout = 10 * time.Second

// runner is the part of *Agent the fixer needs; an interface for tests.
type runner interface {
	Run(ctx context.Context, item *Item) (Result, error)
}

// Fixer runs the agent on submitted torrents one at a time, so moves and
// rescans never overlap.
type Fixer struct {
	agent  runner
	tools  dataPather
	notify Notifier // nil: log only
	queue  chan *Item
}

// dataPather finds a torrent's data on disk (*mediatools.Tools).
type dataPather interface {
	DataPath(t *transmission.Torrent) (string, bool)
}

// NewFixer returns a fixer over agent. notify may be nil.
func NewFixer(agent *Agent, notify Notifier) *Fixer {
	return newFixer(agent, agent.tools, notify)
}

func newFixer(agent runner, tools dataPather, notify Notifier) *Fixer {
	return &Fixer{agent: agent, tools: tools, notify: notify, queue: make(chan *Item, queueSize)}
}

// Submit queues a completed torrent whose "Completed" message replyTo, with
// text notice, was sent to chatID. It returns false when the torrent is
// skipped: a category the fixer does not handle (the notice is left as it
// is), or data not found or a full queue (the notice says so).
func (f *Fixer) Submit(t *transmission.Torrent, chatID, replyTo int64, notice string) bool {
	cat := string(torrentlabels.Category(t.Labels))
	if !slices.Contains(Categories, cat) {
		logger.Debug("[Autofix] skipping %q: category %s", t.Name, cat)
		return false
	}
	item := &Item{TorrentID: t.ID, Name: t.Name, Category: cat, ChatID: chatID, ReplyTo: replyTo, Notice: notice}
	root, ok := f.tools.DataPath(t)
	if !ok {
		logger.Warn("[Autofix] data of %q not found under the download path; skipping", t.Name)
		f.mark(context.Background(), item, StatusNoData)
		return false
	}
	item.Root = root
	// Marked before it is queued, so the worker's "checking" cannot be
	// overwritten by "queued".
	f.mark(context.Background(), item, StatusQueued)
	select {
	case f.queue <- item:
		logger.Info("[Autofix] queued %q (%s)", t.Name, cat)
		return true
	default:
		logger.Warn("[Autofix] queue full; skipping %q", t.Name)
		f.mark(context.Background(), item, StatusBusy)
		return false
	}
}

// mark shows status under item's notice; a failure is only logged (by
// the notifier), since the status is a hint.
func (f *Fixer) mark(ctx context.Context, item *Item, status string) {
	if f.notify == nil {
		return
	}
	markCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), markTimeout)
	defer cancel()
	if err := f.notify.Mark(markCtx, item, status); err != nil {
		logger.Debug("[Autofix] status of %q not shown: %v", item.Name, err)
	}
}

// Start runs queued items until ctx is done. The returned drain waits for
// the run in progress, cancelling it when drainCtx ends (the shutdown
// deadline).
func (f *Fixer) Start(ctx context.Context) (drain func(drainCtx context.Context)) {
	runCtx, cancelRuns := context.WithCancel(context.WithoutCancel(ctx))
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case item := <-f.queue:
				f.handle(runCtx, item)
			}
		}
	}()
	return func(drainCtx context.Context) {
		select {
		case <-done:
		case <-drainCtx.Done():
			logger.Warn("[Autofix] shutdown deadline reached; cancelling the run in progress")
			cancelRuns()
			<-done
		}
		cancelRuns()
	}
}

// handle runs the agent on item, replies with the report when the run
// changed something, needs the owner or failed, and marks the notice with
// the outcome.
func (f *Fixer) handle(ctx context.Context, item *Item) {
	logger.Info("[Autofix] fixing %q", item.Name)
	f.mark(ctx, item, StatusChecking)
	res, err := f.agent.Run(ctx, item)
	if err != nil {
		logger.Error(err, "[Autofix] run for %q", item.Name)
	}
	logger.Info("[Autofix] %q done after %d turn(s), %d change(s), needs input %v: %s",
		item.Name, res.Turns, len(res.Changes), res.NeedsInput, res.Summary)
	status := outcome(res, err)
	if status != StatusClean && f.notify != nil {
		sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
		if err := f.notify.Reply(sendCtx, item, report(item, res, err)); err != nil {
			logger.Error(err, "[Autofix] report for %q not delivered", item.Name)
			status += statusUndelivered
		}
		cancel()
	}
	f.mark(ctx, item, status)
}

// outcome is the status a finished run leaves on the notice.
func outcome(res Result, err error) string {
	switch {
	case err != nil:
		return StatusFailed
	case res.NeedsInput:
		return StatusNeedsInput
	case len(res.Changes) > 0:
		return StatusFixed
	default:
		return StatusClean
	}
}

// telegramLimit is Telegram's maximum message length (characters; bytes
// here, which is stricter).
const telegramLimit = 4096

func report(item *Item, res Result, runErr error) string {
	var b strings.Builder
	if res.NeedsInput && runErr == nil {
		fmt.Fprintf(&b, "Auto-fix needs your decision: %s\n", item.Name)
	} else {
		fmt.Fprintf(&b, "Auto-fix: %s\n", item.Name)
	}
	if runErr != nil {
		fmt.Fprintf(&b, "Stopped: %v\n", runErr)
	}
	if res.Summary != "" {
		b.WriteString("\n" + res.Summary + "\n")
	}
	if len(res.Changes) > 0 {
		b.WriteString("\nChanges:\n")
		for _, c := range res.Changes {
			b.WriteString("• " + c + "\n")
		}
	}
	text := strings.TrimSpace(b.String())
	if len(text) > telegramLimit {
		text = strings.ToValidUTF8(text[:telegramLimit-len("…")], "") + "…"
	}
	return text
}
