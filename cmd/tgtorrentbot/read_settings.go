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

	if problems := validateSettings(settings); len(problems) > 0 {
		return Settings{}, fmt.Errorf("settings file %s problems: %v", settingsPath, problems)
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
	settings.WebhookSecret = os.Getenv("TGT_WEBHOOK_SECRET")

	var problems []string
	if allowedUsersRaw := os.Getenv("TGT_ALLOWED_USERS"); strings.TrimSpace(allowedUsersRaw) != "" {
		allowedUsers, err := parseAllowedUsers(allowedUsersRaw)
		if err != nil {
			problems = append(problems, fmt.Sprintf("TGT_ALLOWED_USERS: %v", err))
		}
		settings.AllowedUsers = allowedUsers
	}
	problems = append(problems, validateSettings(settings)...)

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

// validateSettings returns one problem per missing or invalid value. It runs
// on settings from either source, so each message names the settings-file key
// and the environment variable.
func validateSettings(s Settings) []string {
	var problems []string
	for _, f := range []struct {
		value, name string
	}{
		{s.BotToken, "botToken/TGT_BOTTOKEN"},
		{s.WebHookURL, "webHookURL/TGT_WEBHOOKURL"},
		{s.DownloadPath, "downloadPath/TGT_DOWNLOADPATH"},
		{s.TransmissionRPC.Address, "transmissionRPC.address/TGT_RPC_ADDR"},
		{s.TransmissionRPC.User, "transmissionRPC.user/TGT_RPC_USER"},
		{s.TransmissionRPC.Password, "transmissionRPC.password/TGT_RPC_PASSWORD"},
		{s.RutrackerConfig.Username, "rutrackerConfig.username/TGT_RUTRACKER_USERNAME"},
		{s.RutrackerConfig.Password, "rutrackerConfig.password/TGT_RUTRACKER_PASSWORD"},
	} {
		if strings.TrimSpace(f.value) == "" {
			problems = append(problems, f.name+" is not set")
		}
	}
	// Every string value, the optional ones too: the settings file is read
	// as plain JSON, so a 1Password reference would be used literally, e.g.
	// as the bot token or the web app URL.
	for _, f := range []struct {
		value, name string
	}{
		{s.BotToken, "botToken/TGT_BOTTOKEN"},
		{s.WebHookURL, "webHookURL/TGT_WEBHOOKURL"},
		{s.DownloadPath, "downloadPath/TGT_DOWNLOADPATH"},
		{s.TransmissionRPC.Address, "transmissionRPC.address/TGT_RPC_ADDR"},
		{s.TransmissionRPC.User, "transmissionRPC.user/TGT_RPC_USER"},
		{s.TransmissionRPC.Password, "transmissionRPC.password/TGT_RPC_PASSWORD"},
		{s.RutrackerConfig.Username, "rutrackerConfig.username/TGT_RUTRACKER_USERNAME"},
		{s.RutrackerConfig.Password, "rutrackerConfig.password/TGT_RUTRACKER_PASSWORD"},
		{s.LogLevel, "logLevel/TGT_LOGLEVEL"},
		{s.WebAppURL, "webAppURL/TGT_WEBAPP_URL"},
		{s.WebhookSecret, "webhookSecret/TGT_WEBHOOK_SECRET"},
	} {
		if strings.HasPrefix(strings.TrimSpace(f.value), "op://") {
			problems = append(problems, f.name+" is an unresolved 1Password reference; resolve the file with `op inject` first")
		}
	}
	if len(s.AllowedUsers) == 0 {
		problems = append(problems, "allowedUsers/TGT_ALLOWED_USERS is not set")
	}
	if s.WebhookSecret != "" && !strings.HasPrefix(s.WebhookSecret, "op://") && !validWebhookSecret(s.WebhookSecret) {
		problems = append(problems, "webhookSecret/TGT_WEBHOOK_SECRET must be 1-256 characters of A-Z, a-z, 0-9, _ and -")
	}
	return problems
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
