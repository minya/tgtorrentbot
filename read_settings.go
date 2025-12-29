package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/minya/logger"
)

// ReadSettings reads settings from env or command line or file
func ReadSettings(settingsPath string) (Settings, error) {
	settings, err := readSettingsFromEnv()
	if err == nil {
		logger.Info("Settings read from env")
		return settings, nil
	}

	settings, err = readSettingsFromFile(settingsPath)
	if err == nil {
		logger.Info("Settings read from file")
		return settings, nil
	}

	return Settings{}, fmt.Errorf("Can't get settings")
}

func readSettingsFromEnv() (Settings, error) {
	var settings Settings
	settings.BotToken = os.Getenv("TGT_BOTTOKEN")
	settings.WebHookURL = os.Getenv("TGT_WEBHOOKURL")
	settings.DownloadPath = os.Getenv("TGT_DOWNLOADPATH")
	settings.TransmissionRPC.Address = os.Getenv("TGT_RPC_ADDR")
	settings.TransmissionRPC.User = os.Getenv("TGT_RPC_USER")
	settings.TransmissionRPC.Password = os.Getenv("TGT_RPC_PASSWORD")
	settings.RutrackerConfig.Username = os.Getenv("TGT_RUTRACKER_USERNAME")
	settings.RutrackerConfig.Password = os.Getenv("TGT_RUTRACKER_PASSWORD")
	settings.LogLevel = os.Getenv("TGT_LOGLEVEL")

	var missing []string
	if settings.BotToken == "" {
		missing = append(missing, "TGT_BOTTOKEN")
	}
	if settings.WebHookURL == "" {
		missing = append(missing, "TGT_WEBHOOKURL")
	}
	if settings.DownloadPath == "" {
		missing = append(missing, "TGT_DOWNLOADPATH")
	}
	if settings.TransmissionRPC.Address == "" {
		missing = append(missing, "TGT_RPC_ADDR")
	}
	if settings.TransmissionRPC.User == "" {
		missing = append(missing, "TGT_RPC_USER")
	}
	if settings.TransmissionRPC.Password == "" {
		missing = append(missing, "TGT_RPC_PASSWORD")
	}
	if settings.RutrackerConfig.Username == "" {
		missing = append(missing, "TGT_RUTRACKER_USERNAME")
	}
	if settings.RutrackerConfig.Password == "" {
		missing = append(missing, "TGT_RUTRACKER_PASSWORD")
	}

	if len(missing) > 0 {
		return settings, fmt.Errorf("missing required environment variables: %v", missing)
	}
	return settings, nil
}

func readSettingsFromFile(path string) (Settings, error) {
	settingsBytes, err := os.ReadFile(path)
	var settings Settings
	if err != nil {
		return settings, err
	}
	err = json.Unmarshal(settingsBytes, &settings)
	if err != nil {
		return settings, fmt.Errorf("error unmarshalling settings: %w", err)
	}
	return settings, nil
}
