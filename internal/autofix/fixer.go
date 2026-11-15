package autofix

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/minya/logger"
	"github.com/minya/tgtorrentbot/internal/mediatools"
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
	StatusRestarted   = "Auto-fix: skipped, bot restarted"
	StatusMismatch    = "Auto-fix: skipped, its folder is not in its category"
	statusUndelivered = " (reply not delivered)"
)

// markTimeout bounds one status edit, replyTimeout one report.
const (
	markTimeout  = 10 * time.Second
	replyTimeout = 45 * time.Second
)

// reportBudget is the part of the shutdown deadline kept for the report of
// the run the drain stops: the run is stopped this long before it.
const reportBudget = 2 * time.Second

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
	// reportBudget is reportBudget; tests shorten it.
	reportBudget time.Duration
	// sendBy, once the drain has started, is the shutdown deadline
	// (*time.Time): no report or status edit runs past it.
	sendBy atomic.Pointer[time.Time]
}

// dataPather finds a torrent's data on disk (*mediatools.Tools).
type dataPather interface {
	DataPath(t *transmission.Torrent) (string, bool)
	CategoryDir(category string) string
}

// NewFixer returns a fixer over agent. notify may be nil.
func NewFixer(agent *Agent, notify Notifier) *Fixer {
	return newFixer(agent, agent.tools, notify)
}

func newFixer(agent runner, tools dataPather, notify Notifier) *Fixer {
	return &Fixer{agent: agent, tools: tools, notify: notify, queue: make(chan *Item, queueSize), reportBudget: reportBudget}
}

// Submit queues a completed torrent whose "Completed" message replyTo, with
// text notice, was sent to chatID. It returns false when the torrent is
// skipped: a category the fixer does not handle (the notice is left as it
// is), or data not found or a full queue (the notice says so).
func (f *Fixer) Submit(t *transmission.Torrent, chatID, replyTo int64, notice string) bool {
	cat := torrentlabels.Category(t.Labels)
	if !slices.Contains(Categories, cat) {
		logger.Debug("[Autofix] skipping %q: category %s", t.Name, cat)
		return false
	}
	item := &Item{TorrentID: t.ID, Name: t.Name, Category: string(cat), ChatID: chatID, ReplyTo: replyTo, Notice: notice}
	root, ok := f.tools.DataPath(t)
	if !ok {
		logger.Warn("[Autofix] data of %q not found under the download path; skipping", t.Name)
		f.mark(context.Background(), item, StatusNoData)
		return false
	}
	if !mediatools.IsWithin(root, f.tools.CategoryDir(item.Category)) {
		// The label and the data disagree (relabelled or relocated by
		// hand); a fix would settle that by moving the data to another
		// category. Leave it to the owner.
		logger.Warn("[Autofix] %q is labelled %s but its data is at %s; skipping", t.Name, cat, root)
		f.mark(context.Background(), item, StatusMismatch)
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
	markCtx, cancel := f.sendContext(ctx, markTimeout)
	defer cancel()
	if err := f.notify.Mark(markCtx, item, status); err != nil {
		logger.Debug("[Autofix] status of %q not shown: %v", item.Name, err)
	}
}

// Start runs queued items until ctx is done. The returned drain waits for
// the run in progress under drainCtx's deadline (the shutdown deadline):
// it stops the run reportBudget before it, so the run's report and status
// edit can still go out, and neither runs past the deadline.
func (f *Fixer) Start(ctx context.Context) (drain func(drainCtx context.Context)) {
	runCtx, cancelRuns := context.WithCancel(context.WithoutCancel(ctx))
	done := make(chan struct{})
	// left holds an item taken from the queue after shutdown began; the
	// worker writes it before closing done, the drain reads it after.
	var left []*Item
	go func() {
		defer close(done)
		for {
			// No run starts once shutdown began, even when an item is
			// queued too (select would pick either at random).
			if ctx.Err() != nil {
				return
			}
			select {
			case <-ctx.Done():
				return
			case item := <-f.queue:
				if ctx.Err() != nil {
					left = append(left, item)
					return
				}
				f.handle(runCtx, item)
			}
		}
	}()
	return func(drainCtx context.Context) {
		defer cancelRuns()
		if deadline, ok := drainCtx.Deadline(); ok {
			f.sendBy.Store(&deadline)
			stop := time.AfterFunc(time.Until(deadline.Add(-f.reportBudget)), func() {
				logger.Warn("[Autofix] shutdown: stopping the run in progress")
				cancelRuns()
			})
			defer stop.Stop()
		}
		select {
		case <-done:
			f.skipQueued(drainCtx, left)
		case <-drainCtx.Done():
			cancelRuns()
			logger.Warn("[Autofix] shutdown deadline reached before the run in progress ended")
		}
	}
}

// skipQueued tells the chats of the items that were queued but never run
// that their downloads were not checked, as far as the deadline allows,
// and logs them.
func (f *Fixer) skipQueued(drainCtx context.Context, left []*Item) {
	for {
		select {
		case item := <-f.queue:
			left = append(left, item)
			continue
		default:
		}
		break
	}
	for _, item := range left {
		logger.Warn("[Autofix] not checked, the bot is stopping: %q", item.Name)
		if drainCtx.Err() == nil {
			f.mark(drainCtx, item, StatusRestarted)
		}
	}
}

// sendContext bounds a Telegram call of the fixer by timeout and, once the
// drain has started, by the shutdown deadline. It is detached from ctx, so
// a stopped run can still report.
func (f *Fixer) sendContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	if by := f.sendBy.Load(); by != nil {
		byCtx, cancelBy := context.WithDeadline(sendCtx, *by)
		return byCtx, func() { cancelBy(); cancel() }
	}
	return sendCtx, cancel
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
		sendCtx, cancel := f.sendContext(ctx, replyTimeout)
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
		fmt.Fprintf(&b, "Stopped: %s\n", stopReason(runErr))
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

// stopReason words a run's error for the owner: a cancelled run was
// interrupted by a restart, not by a raw "context canceled".
func stopReason(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "interrupted by a restart of the bot"
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Sprintf("took longer than %v", runTimeout)
	default:
		return err.Error()
	}
}
