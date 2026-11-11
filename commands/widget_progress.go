package commands

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/minya/logger"
	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/environment"
	"github.com/minya/tgtorrentbot/internal/torrentlabels"
	"github.com/odwrtw/transmission"
)

// The download progress widget (Bot API widgets,
// https://core.telegram.org/bots/widgets): sent instead of the "Added"
// message in the private chat, it shows the torrent's progress and, while
// it is on screen, asks the bot for fresh numbers every 5 seconds with a
// widget query "p:<hash>". The answer is the same JSON as the widget's chat
// state (widgetState). Once the torrent is done or removed, the bot also
// writes that final state into the message's chat_data, so the widget shows
// it without asking whenever it is drawn again, and stops asking.
//
// The torrent is named by its info hash, not its Transmission id: ids are
// renumbered when Transmission restarts, and a widget message lives on.

//go:embed progress_widget.html
var progressWidgetHTML string

// maxWidgetName bounds the name in the state: chat_data and the answer are
// capped at 1024 bytes each.
const maxWidgetName = 400

var reWidgetProgress = regexp.MustCompile(`^p:([0-9a-fA-F]{40})$`)

// widgetState is the widget's view of a torrent. Short keys keep it far
// below the 1024-byte limit.
type widgetState struct {
	Hash     string  `json:"h"`
	Name     string  `json:"n,omitempty"`
	Category string  `json:"c,omitempty"`
	Size     int64   `json:"z,omitempty"`
	Done     float64 `json:"p"`
	Status   string  `json:"s"`
	Rate     int     `json:"r,omitempty"`
	ETA      int     `json:"e,omitempty"`
	Error    string  `json:"x,omitempty"`
}

// Values of widgetState.Status.
const (
	widgetDownloading = "dl"
	widgetQueued      = "wait"
	widgetChecking    = "check"
	widgetStopped     = "stop"
	widgetDone        = "done"
	widgetGone        = "gone" // no longer in Transmission
)

func (s widgetState) final() bool { return s.Status == widgetDone || s.Status == widgetGone }

func (s widgetState) json() string {
	b, _ := json.Marshal(s) // plain strings and numbers: cannot fail
	return string(b)
}

// truncateUTF8 cuts s to at most n bytes without splitting a character.
func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s + "…"
}

// torrentWidgetState describes t.
func torrentWidgetState(t *transmission.Torrent) widgetState {
	category := torrentlabels.Category(t.Labels)
	st := widgetState{
		Hash:     strings.ToLower(t.HashString),
		Name:     truncateUTF8(t.Name, maxWidgetName),
		Category: category.DisplayName(),
		Size:     t.SizeWhenDone,
		Done:     t.PercentDone,
		Rate:     t.RateDownload,
		ETA:      t.Eta,
		Error:    truncateUTF8(t.ErrorString, 200),
	}
	switch {
	case t.PercentDone >= 1:
		st.Status, st.Rate, st.ETA, st.Error = widgetDone, 0, 0, ""
	case t.Status == transmission.StatusStopped:
		st.Status = widgetStopped
	case t.Status == transmission.StatusCheckPending, t.Status == transmission.StatusChecking:
		st.Status = widgetChecking
	case t.Status == transmission.StatusDownloadPending:
		st.Status = widgetQueued
	default:
		st.Status = widgetDownloading
	}
	if st.ETA < 0 { // Transmission's "unknown" and "not available"
		st.ETA = 0
	}
	return st
}

// sendProgressWidget sends the progress widget for a torrent just added.
// The caption is what the chat list, notifications and apps without widget
// support show.
func sendProgressWidget(ctx context.Context, api *telegram.API, chatID int64, t *transmission.Torrent, category Category) error {
	st := widgetState{
		Hash:     strings.ToLower(t.HashString),
		Name:     truncateUTF8(t.Name, maxWidgetName),
		Category: category.DisplayName(),
		Status:   widgetDownloading,
	}
	return withFloodRetry(ctx, func() error {
		_, err := api.SendWidget(ctx, telegram.SendWidgetParams{
			ChatID:       chatID,
			HTML:         progressWidgetHTML,
			ChatData:     st.json(),
			Caption:      truncateUTF8(fmt.Sprintf("Added %d: %s [%s]", t.ID, t.Name, category.DisplayName()), 1000),
			AllowQueries: true,
		})
		return err
	})
}

type WidgetProgressCommandFactory struct {
	environment.Env
}

type WidgetProgressCommand struct {
	Hash string
	environment.Env
}

func (f *WidgetProgressCommandFactory) Accepts(upd *telegram.Update) (bool, Command) {
	if upd == nil || upd.WidgetQuery == nil {
		return false, nil
	}
	m := reWidgetProgress.FindStringSubmatch(upd.WidgetQuery.Data)
	if m == nil {
		return false, nil
	}
	return true, &WidgetProgressCommand{Hash: strings.ToLower(m[1]), Env: f.Env}
}

// Handle answers the widget with the torrent's current state, and records
// a final state in the message so the widget stops asking.
func (cmd *WidgetProgressCommand) Handle(ctx context.Context, upd *telegram.Update) error {
	q := upd.WidgetQuery
	torrents, err := cmd.TransmissionClient.GetTorrents()
	if err != nil {
		// Answer anyway, or the widget waits 15 s for a TIMEOUT. It keeps
		// its last state and asks again.
		AnswerWidget(ctx, cmd.TgApi, q.ID, "")
		return Replied(fmt.Errorf("widget query: list torrents: %w", err))
	}
	st := widgetState{Hash: cmd.Hash, Status: widgetGone}
	for _, t := range torrents {
		if strings.EqualFold(t.HashString, cmd.Hash) {
			st = torrentWidgetState(t)
			break
		}
	}
	// A removed torrent's state has only its hash: the query carries no
	// state, and the widget keeps the name it already shows.
	AnswerWidget(ctx, cmd.TgApi, q.ID, st.json())

	if st.final() && q.Chat != nil && q.MessageID != 0 {
		data := st.json()
		err := withFloodRetry(ctx, func() error {
			return cmd.TgApi.EditMessageWidgetData(ctx, telegram.EditMessageWidgetDataParams{
				ChatID: q.Chat.ID, MessageID: q.MessageID, ChatData: &data,
			})
		})
		if err != nil && !strings.Contains(err.Error(), "message is not modified") {
			logger.Error(err, "Failed to store the final widget state in message %d", q.MessageID)
			return Replied(err)
		}
	}
	return nil
}

// AnswerWidget answers a widget query and logs a failure.
func AnswerWidget(ctx context.Context, api *telegram.API, queryID, data string) {
	if err := api.AnswerWidgetQuery(ctx, telegram.AnswerWidgetQueryParams{WidgetQueryID: queryID, Data: data}); err != nil {
		logger.Error(err, "Failed to answer widget query %s", queryID)
	}
}
