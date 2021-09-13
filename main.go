package main

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"time"

	"github.com/minya/telegram"
	"github.com/odwrtw/transmission"
)

func main() {
	settingsBytes, err := ioutil.ReadFile("settings.json")
	if err != nil {
		log.Fatal(err)
	}
	var settings Settings
	json.Unmarshal(settingsBytes, &settings)
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
	handler := UpdatesHandler{
		transmissionClient: transmissionClient,
		tgApi:              &api,
		downloadPath:       settings.DownloadPath,
	}
	log.Fatal(telegram.StartPolling(&api, handler.HandleUpdate, 3*time.Second, -1))
}
