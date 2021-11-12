package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
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
	handler := UpdatesHandler{
		transmissionClient: transmissionClient,
		tgApi:              &api,
		downloadPath:       settings.DownloadPath,
	}
	log.Fatal(telegram.StartPolling(&api, handler.HandleUpdate, 3*time.Second, -1))
}

func readSettings() (Settings, error) {
	settings, err := readSettingsFromEnv()
	if err == nil {
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
