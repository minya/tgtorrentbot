package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/minya/logger"
	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/commands"
	"github.com/minya/tgtorrentbot/internal/completion"
	"github.com/minya/tgtorrentbot/internal/torrentlabels"
	"github.com/odwrtw/transmission"
)

// CreateCompletedCheckRoutine starts the completion checker and returns a
// function that triggers an immediate check (e.g. right after a download was
// added). The checker also polls Transmission every minute on its own, so
// torrents added from the webapp or outside the bot are observed too.
//
// Completions that happen while the bot is not running are not notified:
// the first check only reports torrents that completed after the bot started.
//
// onCompleted, when set, is called for every completion whose notice was
// delivered, with the chat and the id of the notice (the auto-fixer starts
// there and replies to it).
//
// The checker stops when ctx is done, after finishing a check in progress
// (its notices are not cut mid-send); done is closed when it has stopped.
func CreateCompletedCheckRoutine(ctx context.Context, transmissionClient *transmission.Client, api *telegram.API, onCompleted completedFunc) (notify func(), done <-chan struct{}) {
	chanNotify := make(chan int, 1)
	stopped := make(chan struct{})

	updateFn := func() {
		defer close(stopped)
		var state transmission.TorrentMap
		lastPoll := time.Now().Unix()
		checkTorrents := func() {
			pollStart := time.Now().Unix()
			newState, err := updateCheckRoutine(transmissionClient, api, state, lastPoll, onCompleted)
			if err != nil {
				logger.Error(err, "[UpdatesChecker] Error checking torrents")
				return
			}
			state = newState
			lastPoll = pollStart
			logger.Debug("[UpdatesChecker] Completed check cycle")
		}
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		checkTorrents()
		for {
			select {
			case <-ctx.Done():
				return
			case <-chanNotify:
				logger.Debug("[UpdatesChecker] Check requested")
				checkTorrents()
			case <-ticker.C:
				checkTorrents()
			}
		}
	}

	go updateFn()

	notifyFunc := func() {
		select {
		case chanNotify <- 1:
		default:
		}
	}

	return notifyFunc, stopped
}

// updateCheckRoutine fetches the torrents, notifies the chat of every torrent
// that completed since the previous check, and returns the new state.
// since is the Unix time of the previous successful poll (or of the start of
// the checker before the first one).
func updateCheckRoutine(
	transmissionClient *transmission.Client,
	api *telegram.API,
	state transmission.TorrentMap,
	since int64,
	onCompleted completedFunc,
) (transmission.TorrentMap, error) {
	torrents, err := transmissionClient.GetTorrentMap()

	if err != nil {
		logger.Error(err, "[UpdatesChecker] Error getting torrent map")
		return state, err
	}

	for _, torrent := range completion.NewlyCompleted(state, torrents, since) {
		chatID, err := getTorrentChatID(torrent)
		if err != nil {
			logger.Warn("[UpdatesChecker] No chat ID found for torrent %s, error: %v", torrent.Name, err)
			continue
		}

		logger.Info("[UpdatesChecker] Found completed torrent: %s", torrent.Name)

		category := getTorrentCategory(torrent)
		ctx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
		notice := fmt.Sprintf("Completed: %v [%s]", torrent.Name, category)
		messageID, err := commands.SendMessageGetID(ctx, api, telegram.SendMessageParams{
			ChatID: chatID,
			Text:   notice,
		})
		cancel()
		if err == nil && onCompleted != nil {
			onCompleted(torrent, chatID, messageID, notice)
		}
		if err != nil && transientSendError(err) {
			// Record the torrent as still incomplete, so the next check
			// sees the transition again and retries the notification.
			retry := *torrent
			retry.PercentDone = 0
			torrents[torrent.HashString] = &retry
			logger.Warn("[UpdatesChecker] Completion notice for %s not delivered, will retry: %v", torrent.Name, err)
		}
	}

	return torrents, nil
}

// completedFunc receives a torrent whose completion notice messageID, with
// text notice, was delivered to chatID.
type completedFunc func(torrent *transmission.Torrent, chatID, messageID int64, notice string)

// notifyTimeout bounds one completion notification, its flood-control
// retry included.
const notifyTimeout = 45 * time.Second

// transientSendError reports whether a failed send is worth retrying on the
// next check: network errors, timeouts, 429 and 5xx are; a 400 or 403 (bad
// chat, bot blocked by the user) would fail forever.
func transientSendError(err error) bool {
	var tgErr *telegram.Error
	if errors.As(err, &tgErr) {
		return tgErr.Code == http.StatusTooManyRequests || tgErr.Code >= 500
	}
	return true
}

func getTorrentChatID(torrent *transmission.Torrent) (int64, error) {
	if len(torrent.Labels) == 0 {
		return 0, fmt.Errorf("no chatID in torrent's labels")
	}

	chatID, err := strconv.ParseInt(torrent.Labels[0], 10, 64)
	if err != nil {
		return 0, err
	}
	return chatID, nil
}

func getTorrentCategory(torrent *transmission.Torrent) string {
	return torrentlabels.Category(torrent.Labels).DisplayName()
}
