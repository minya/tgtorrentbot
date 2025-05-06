package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/minya/telegram"
	"github.com/odwrtw/transmission"
)

func main() {
	settingsPath := flag.String("settings", "settings.json", "Path to settings file")
	flag.Parse()

	settings, err := ReadSettings(*settingsPath)
	if err != nil {
		log.Fatal(err)
	}

	conf := transmission.Config{
		Address:  settings.TransmissionRPC.Address,
		User:     settings.TransmissionRPC.User,
		Password: settings.TransmissionRPC.Password,
	}
	transmissionClient, err := transmission.New(conf)
	if err != nil {
		log.Fatal(fmt.Sprintf("Can't create transmission client: %v", err))
		panic(err)
	}

	api := telegram.NewApi(settings.BotToken)
	updateRoutine, chanNotify := CreateCompletedCheckRoutine(transmissionClient, &api)
	go updateRoutine()

	handler := UpdatesHandler{
		transmissionClient: transmissionClient,
		tgApi:              &api,
		downloadPath:       settings.DownloadPath,
		rutrackerConfig:    &settings.RutrackerConfig,
		notify:             func() { chanNotify <- 1 },
	}

	log.Println("Bot started")
	log.Fatal(telegram.StartPolling(&api, handler.HandleUpdate, 3*time.Second, -1))
}
