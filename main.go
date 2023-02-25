package main

import (
	"log"
	"time"

	"github.com/minya/telegram"
	"github.com/odwrtw/transmission"
)

func main() {
	settings, err := ReadSettings()
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

	updateRoutine, chanNotify := CreateCompletedCheckRoutine(transmissionClient, &api)
	go updateRoutine()

	handler := UpdatesHandler{
		transmissionClient: transmissionClient,
		tgApi:              &api,
		downloadPath:       settings.DownloadPath,
		rutrackerConfig:    &settings.RutrackerConfig,
	}
	log.Fatal(telegram.StartPolling(&api, handler.HandleUpdate, 3*time.Second, -1, chanNotify))
}
