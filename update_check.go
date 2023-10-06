package main

import (
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/minya/telegram"
	"github.com/odwrtw/transmission"
)

func CreateCompletedCheckRoutine(transmissionClient *transmission.Client, api *telegram.Api) (func(), chan int) {
	chanNotify := make(chan int)

	updateFn := func() {
		var globalTorrentState transmission.TorrentMap
		for {
			<-chanNotify
			log.Printf("[UpdatesChecker] Start\n")
			for {
				newState, err := updateCheckRoutine(transmissionClient, api, globalTorrentState)
				if err == nil {
					globalTorrentState = newState
					if allCompleted(globalTorrentState) {
						log.Printf("[UpdatesChecker] All torrents completed\n")
					}
				} else {
					log.Printf("[UpdatesChecker] Error %v\n", err)
				}
				log.Printf("[UpdatesChecker] Completed\n")
				time.Sleep(1 * time.Minute)
			}
		}
	}

	return updateFn, chanNotify
}

func updateCheckRoutine(
	transmissionClient *transmission.Client,
	api *telegram.Api,
	state transmission.TorrentMap,
) (transmission.TorrentMap, error) {
	torrents, err := transmissionClient.GetTorrentMap()

	if err != nil {
		log.Printf("[UpdatesChecker] error %v\n", err)
		return state, err
	}

	for hash, torrent := range torrents {
		log.Printf("[UpdatesChecker] Check %v\n", torrent.Name)
		previous, ok := state[hash]
		if !ok {
			previous = torrent
		}
		if torrent.PercentDone == 1 && previous.PercentDone < 1 {
			chatID, err := getTorrentChatID(torrent)
			if err != nil {
				log.Printf("[UpdatesChecker] Warn  %v: no chat id found (%v)\n", torrent.Name, err)
				continue
			}

			log.Printf("[UpdatesChecker] Found completed torent %v\n", torrent.Name)

			api.SendMessage(telegram.ReplyMessage{
				ChatId: chatID,
				Text:   fmt.Sprintf("Завершено: %v", torrent.Name),
			})
		}
	}

	return torrents, nil
}

func getTorrentChatID(torrent *transmission.Torrent) (int, error) {
	if len(torrent.Labels) == 0 {
		return 0, fmt.Errorf("no chatID in torrent's labels")
	}

	chatID, err := strconv.Atoi(torrent.Labels[0])
	if err != nil {
		return 0, err
	}
	return chatID, nil
}

func allCompleted(torrents transmission.TorrentMap) bool {
	for _, torrent := range torrents {
		if torrent.PercentDone < 1 {
			return false
		}
	}
	return true
}
