package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/minya/logger"
)

type configNotAttemptedError struct{}

func (e configNotAttemptedError) Error() string {
	return "no TGT_* environment variables set"
}

// ReadSettings reads settings from env or command line or file
func ReadSettings(settingsPath string) (Settings, error) {
	settings, err := readSettingsFromEnv()
	if err == nil {
		logger.Info("Settings read from env")
		return settings, nil
	}

	var notAttempted configNotAttemptedError
	if !errors.As(err, &notAttempted) {
		return Settings{}, fmt.Errorf("environment config error: %w", err)
	}

	settings, err = readSettingsFromFile(settingsPath)
	if err != nil {
		return Settings{}, fmt.Errorf("can't get settings from file: %w", err)
	}

	if err := validateSettings(settings); err != nil {
		return Settings{}, fmt.Errorf("file config validation error: %w", err)
	}

	logger.Info("Settings read from file")
	return settings, nil
}

var requiredEnvVars = []string{
	"TGT_BOTTOKEN",
	"TGT_WEBHOOKURL",
	"TGT_DOWNLOADPATH",
	"TGT_RPC_ADDR",
	"TGT_RPC_USER",
	"TGT_RPC_PASSWORD",
	"TGT_RUTRACKER_USERNAME",
	"TGT_RUTRACKER_PASSWORD",
	"TGT_ALLOWED_USERS",
}

func readSettingsFromEnv() (Settings, error) {
	anySet := false
	for _, key := range requiredEnvVars {
		if os.Getenv(key) != "" {
			anySet = true
			break
		}
	}
	if !anySet {
		return Settings{}, configNotAttemptedError{}
	}

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
	settings.WebAppURL = os.Getenv("TGT_WEBAPP_URL")

	var problems []string
	if settings.BotToken == "" {
		problems = append(problems, "TGT_BOTTOKEN is not set")
	}
	if settings.WebHookURL == "" {
		problems = append(problems, "TGT_WEBHOOKURL is not set")
	}
	if settings.DownloadPath == "" {
		problems = append(problems, "TGT_DOWNLOADPATH is not set")
	}
	if settings.TransmissionRPC.Address == "" {
		problems = append(problems, "TGT_RPC_ADDR is not set")
	}
	if settings.TransmissionRPC.User == "" {
		problems = append(problems, "TGT_RPC_USER is not set")
	}
	if settings.TransmissionRPC.Password == "" {
		problems = append(problems, "TGT_RPC_PASSWORD is not set")
	}
	if settings.RutrackerConfig.Username == "" {
		problems = append(problems, "TGT_RUTRACKER_USERNAME is not set")
	}
	if settings.RutrackerConfig.Password == "" {
		problems = append(problems, "TGT_RUTRACKER_PASSWORD is not set")
	}

	allowedUsersRaw := os.Getenv("TGT_ALLOWED_USERS")
	if strings.TrimSpace(allowedUsersRaw) == "" {
		problems = append(problems, "TGT_ALLOWED_USERS is not set")
	} else {
		allowedUsers, err := parseAllowedUsers(allowedUsersRaw)
		if err != nil {
			problems = append(problems, fmt.Sprintf("TGT_ALLOWED_USERS: %v", err))
		}
		settings.AllowedUsers = allowedUsers
	}

	if len(problems) > 0 {
		return settings, fmt.Errorf("environment config problems: %v", problems)
	}
	return settings, nil
}

func parseAllowedUsers(s string) ([]int64, error) {
	var result []int64
	var invalid []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			invalid = append(invalid, part)
			continue
		}
		result = append(result, id)
	}
	if len(invalid) > 0 {
		return result, fmt.Errorf("invalid user IDs: %v", invalid)
	}
	return result, nil
}

func validateSettings(s Settings) error {
	if len(s.AllowedUsers) == 0 {
		return fmt.Errorf("allowedUsers must not be empty")
	}
	return nil
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
