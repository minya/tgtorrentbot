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

// Reporter delivers a run's report to the chat of the item.
type Reporter func(ctx context.Context, item *Item, text string) error

// runner is the part of *Agent the fixer needs; an interface for tests.
type runner interface {
	Run(ctx context.Context, item *Item) (Result, error)
}

// Fixer runs the agent on submitted torrents one at a time, so moves and
// rescans never overlap.
type Fixer struct {
	agent  runner
	tools  dataPather
	report Reporter // nil: log only
	queue  chan *Item
}

// dataPather finds a torrent's data on disk (*mediatools.Tools).
type dataPather interface {
	DataPath(t *transmission.Torrent) (string, bool)
}

// NewFixer returns a fixer over agent. report may be nil.
func NewFixer(agent *Agent, report Reporter) *Fixer {
	return newFixer(agent, agent.tools, report)
}

func newFixer(agent runner, tools dataPather, report Reporter) *Fixer {
	return &Fixer{agent: agent, tools: tools, report: report, queue: make(chan *Item, queueSize)}
}

// Submit queues a completed torrent whose "Completed" message replyTo was
// sent to chatID. It returns false when the torrent is skipped (category
// not handled, data not found, queue full).
func (f *Fixer) Submit(t *transmission.Torrent, chatID, replyTo int64) bool {
	cat := string(torrentlabels.Category(t.Labels))
	if !slices.Contains(Categories, cat) {
		logger.Debug("[Autofix] skipping %q: category %s", t.Name, cat)
		return false
	}
	root, ok := f.tools.DataPath(t)
	if !ok {
		logger.Warn("[Autofix] data of %q not found under the download path; skipping", t.Name)
		return false
	}
	item := &Item{TorrentID: t.ID, Name: t.Name, Category: cat, ChatID: chatID, ReplyTo: replyTo, Root: root}
	select {
	case f.queue <- item:
		logger.Info("[Autofix] queued %q (%s)", t.Name, cat)
		return true
	default:
		logger.Warn("[Autofix] queue full; skipping %q", t.Name)
		return false
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

// handle runs the agent on item and reports the outcome when it changed
// something or failed.
func (f *Fixer) handle(ctx context.Context, item *Item) {
	logger.Info("[Autofix] fixing %q", item.Name)
	res, err := f.agent.Run(ctx, item)
	if err != nil {
		logger.Error(err, "[Autofix] run for %q", item.Name)
	}
	logger.Info("[Autofix] %q done after %d turn(s), %d change(s): %s", item.Name, res.Turns, len(res.Changes), res.Summary)
	if err == nil && len(res.Changes) == 0 {
		return
	}
	if f.report == nil {
		return
	}
	sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
	defer cancel()
	if err := f.report(sendCtx, item, report(item, res, err)); err != nil {
		logger.Error(err, "[Autofix] report for %q not delivered", item.Name)
	}
}

// telegramLimit is Telegram's maximum message length (characters; bytes
// here, which is stricter).
const telegramLimit = 4096

func report(item *Item, res Result, runErr error) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Auto-fix: %s\n", item.Name)
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
