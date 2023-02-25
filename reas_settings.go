package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
)

func ReadSettings() (Settings, error) {
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
