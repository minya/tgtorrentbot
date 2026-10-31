package main

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/minya/logger"
	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/commands"
	"github.com/odwrtw/transmission"
)

// CreateCompletedCheckRoutine starts the completion checker and returns a
// function that triggers an immediate check (e.g. right after a download was
// added). The checker also polls Transmission every minute on its own, so
// torrents added from the webapp or outside the bot are observed too.
//
// Completions that happen while the bot is not running are not notified:
// the first check only reports torrents that completed after the bot started.
func CreateCompletedCheckRoutine(transmissionClient *transmission.Client, api *telegram.API) func() {
	chanNotify := make(chan int, 1)

	updateFn := func() {
		var state transmission.TorrentMap
		lastPoll := time.Now().Unix()
		checkTorrents := func() {
			pollStart := time.Now().Unix()
			newState, err := updateCheckRoutine(transmissionClient, api, state, lastPoll)
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

	return notifyFunc
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
) (transmission.TorrentMap, error) {
	torrents, err := transmissionClient.GetTorrentMap()

	if err != nil {
		logger.Error(err, "[UpdatesChecker] Error getting torrent map")
		return state, err
	}

	for _, torrent := range newlyCompleted(state, torrents, since) {
		chatID, err := getTorrentChatID(torrent)
		if err != nil {
			logger.Warn("[UpdatesChecker] No chat ID found for torrent %s, error: %v", torrent.Name, err)
			continue
		}

		logger.Info("[UpdatesChecker] Found completed torrent: %s", torrent.Name)

		category := getTorrentCategory(torrent)
		api.SendMessage(context.Background(), telegram.SendMessageParams{
			ChatID: chatID,
			Text:   fmt.Sprintf("Completed: %v [%s]", torrent.Name, category),
		})
	}

	return torrents, nil
}

// newlyCompleted returns the torrents of current that completed since the
// previous check. A torrent seen before completes when its PercentDone goes
// from below 1 to 1. A torrent not seen before (first check, added from the
// webapp or outside the bot, or added and completed between two checks)
// counts when Transmission's doneDate is at or after since, so torrents that
// were already complete before the checker started are not reported.
func newlyCompleted(previous, current transmission.TorrentMap, since int64) []*transmission.Torrent {
	var done []*transmission.Torrent
	for hash, torrent := range current {
		if torrent.PercentDone < 1 {
			continue
		}
		if prev, ok := previous[hash]; ok {
			if prev.PercentDone < 1 {
				done = append(done, torrent)
			}
			continue
		}
		if torrent.DoneDate > 0 && int64(torrent.DoneDate) >= since {
			done = append(done, torrent)
		}
	}
	return done
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
	if len(torrent.Labels) >= 2 {
		if cat, ok := commands.ParseCategory(torrent.Labels[1]); ok {
			return cat.DisplayName()
		}
	}
	return "Unknown"
}
