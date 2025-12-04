package main

import (
	"fmt"
	"strconv"
	"time"

	"github.com/minya/logger"
	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/commands"
	"github.com/odwrtw/transmission"
)

func CreateCompletedCheckRoutine(transmissionClient *transmission.Client, api *telegram.Api) func() {
	chanNotify := make(chan int, 1)

	updateFn := func() {
		var globalTorrentState transmission.TorrentMap
		active := false
		checkTorrents := func() {
			newState, err := updateCheckRoutine(transmissionClient, api, globalTorrentState)
			if err == nil {
				globalTorrentState = newState
				if allCompleted(globalTorrentState) {
					logger.Info("[UpdatesChecker] All torrents completed. Update checking paused.")
					active = false
				}
			} else {
				logger.Error(err, "[UpdatesChecker] Error checking torrents")
			}
			logger.Info("[UpdatesChecker] Completed check cycle")
		}
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-chanNotify:
				logger.Info("[UpdatesChecker] Starting torrent completion check")
				active = true
				checkTorrents()

			case <-ticker.C:
				if active {
					checkTorrents()
				}
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

func updateCheckRoutine(
	transmissionClient *transmission.Client,
	api *telegram.Api,
	state transmission.TorrentMap,
) (transmission.TorrentMap, error) {
	torrents, err := transmissionClient.GetTorrentMap()

	if err != nil {
		logger.Error(err, "[UpdatesChecker] Error getting torrent map")
		return state, err
	}

	for hash, torrent := range torrents {
		logger.Debug("[UpdatesChecker] Check torrent: %s", torrent.Name)
		previous, ok := state[hash]
		if !ok {
			previous = torrent
		}
		if torrent.PercentDone == 1 && previous.PercentDone < 1 {
			chatID, err := getTorrentChatID(torrent)
			if err != nil {
				logger.Warn("[UpdatesChecker] No chat ID found for torrent %s, error: %v", torrent.Name, err)
				continue
			}

			logger.Info("[UpdatesChecker] Found completed torrent: %s", torrent.Name)

			category := getTorrentCategory(torrent)
			api.SendMessage(telegram.ReplyMessage{
				ChatId: chatID,
				Text:   fmt.Sprintf("Завершено: %v [%s]", torrent.Name, category), // TODO: tranlate
			})
		}
	}

	return torrents, nil
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

func allCompleted(torrents transmission.TorrentMap) bool {
	for _, torrent := range torrents {
		if torrent.PercentDone < 1 {
			return false
		}
	}
	return true
}
