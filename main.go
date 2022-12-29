package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/minya/telegram"
	"github.com/odwrtw/transmission"
)

func main() {
	settings, err := readSettings()
	if err != nil {
		log.Fatal(err)
	}
	api := telegram.NewApi(settings.BotToken)

	conf := transmission.Config{
		Address:  settings.TransmissionRPC.Address,
		User:     settings.TransmissionRPC.User,
		Password: settings.TransmissionRPC.Password,
	}
	transmissionClient, err := transmission.New(conf)
	if err != nil {
		log.Fatal(err)
		panic(err)
	}
	var globalTorrentState *transmission.TorrentMap
	checkCompleted := func() {
		for {
			log.Printf("[UpdatesChecker] Start check\n")
			torrents, err := transmissionClient.GetTorrentMap()
			if err == nil {
				if globalTorrentState != nil {
					for hash, torrent := range torrents {
						log.Printf("[UpdatesChecker] Check %v\n", torrent.Name)
						previous, ok := (*globalTorrentState)[hash]
						if torrent.PercentDone == 1 && (!ok || previous.PercentDone < 1) {
							chatID, err := getTorrentChatID(torrent)
							if err != nil {
								log.Printf("[UpdatesChecker] %v Error %v\n", torrent.Name, err)
								continue
							}
							log.Printf("[UpdatesChecker] Found completed torent %v\n", torrent.Name)
							api.SendMessage(telegram.ReplyMessage{
								ChatId: chatID,
								Text:   fmt.Sprintf("Завершено: %v", torrent.Name),
							})
						}
					}
				}
				globalTorrentState = &torrents
			} else {
				log.Printf("[UpdatesChecker] error %v\n", err)
			}
			log.Printf("[UpdatesChecker] Completed\n")
			time.Sleep(1 * time.Minute)
		}
	}
	handler := UpdatesHandler{
		transmissionClient: transmissionClient,
		tgApi:              &api,
		downloadPath:       settings.DownloadPath,
		rutrackerConfig:    &settings.RutrackerConfig,
	}

	go checkCompleted()

	log.Fatal(telegram.StartPolling(&api, handler.HandleUpdate, 3*time.Second, -1))
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

func readSettings() (Settings, error) {
	settings, err := readSettingsFromEnv()
	if err == nil {
		fmt.Printf("settings %v\n", settings)
		return settings, nil
	}
	settings, err = readSettingsFromFile()
	if err == nil {
		return settings, nil
	}
	return Settings{}, fmt.Errorf("cant get settings")
}

func readSettingsFromEnv() (Settings, error) {
	var settings Settings
	settings.BotToken = os.Getenv("TGT_BOTTOKEN")
	settings.DownloadPath = os.Getenv("TGT_DOWNLOADPATH")
	settings.TransmissionRPC.Address = os.Getenv("TGT_RPC_ADDR")
	settings.TransmissionRPC.User = os.Getenv("TGT_RPC_USER")
	settings.TransmissionRPC.Password = os.Getenv("TGT_RPC_PASSWORD")
	settings.RutrackerConfig.Username = os.Getenv("TGT_RUTRACKER_USERNAME")
	settings.RutrackerConfig.Password = os.Getenv("TGT_RUTRACKER_PASSWORD")

	if settings.BotToken == "" || settings.DownloadPath == "" || settings.TransmissionRPC.Address == "" {
		return settings, fmt.Errorf("can't get settings from env")
	}
	return settings, nil
}

func readSettingsFromFile() (Settings, error) {
	settingsBytes, err := ioutil.ReadFile("settings.json")
	var settings Settings
	if err != nil {
		return settings, err
	}
	json.Unmarshal(settingsBytes, &settings)
	return settings, nil
}
