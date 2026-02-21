package main

import (
	"encoding/json"
	"flag"
	"net/http"
	"os"

	"github.com/minya/logger"
	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/environment"
	"github.com/odwrtw/transmission"
)

func main() {
	settingsPath := flag.String("settings", "settings.json", "Path to settings file")
	logLevelFlag := flag.String("log-level", "", "Log level (debug, info, warn, error) - overrides settings file/env")
	prettyLog := flag.Bool("pretty-log", true, "Enable pretty logging")
	flag.Parse()

	settings, err := ReadSettings(*settingsPath)
	if err != nil {
		logger.Fatal(err, "Failed to read settings")
	}

	// Determine log level priority: flag > settings > default
	logLevel := "info"
	if *logLevelFlag != "" {
		logLevel = *logLevelFlag
	} else if settings.LogLevel != "" {
		logLevel = settings.LogLevel
	}

	logger.InitLogger(logger.Config{
		Level:      logLevel,
		Pretty:     *prettyLog,
		WithCaller: true,
	})

	conf := transmission.Config{
		Address:  settings.TransmissionRPC.Address,
		User:     settings.TransmissionRPC.User,
		Password: settings.TransmissionRPC.Password,
	}
	transmissionClient, err := transmission.New(conf)
	if err != nil {
		logger.Fatal(err, "Can't create transmission client")
	}

	api := telegram.NewApi(settings.BotToken)
	notify := CreateCompletedCheckRoutine(transmissionClient, &api)

	env := environment.Env{
		TransmissionClient: transmissionClient,
		TgApi:              &api,
		DownloadPath:       settings.DownloadPath,
		RutrackerConfig:    &settings.RutrackerConfig,
		WebAppURL:          settings.WebAppURL,
	}

	handler := NewUpdatesHandler(env, notify)


	webhookParams := telegram.SetWebhookParams{
		Url:         settings.WebHookURL,
	}

	err = api.SetWebhook(&webhookParams)
	if err != nil {
		logger.Fatal(err, "Failed to set webhook")
	}

	// Set menu button to open webapp
	if settings.WebAppURL != "" {
		menuButtonParams := telegram.SetChatMenuButtonParams{
			MenuButton: &telegram.MenuButton{
				Type: "web_app",
				Text: "Open",
				WebApp: &telegram.WebAppInfo{
					Url: settings.WebAppURL,
				},
			},
		}
		err = api.SetChatMenuButton(&menuButtonParams)
		if err != nil {
			logger.Error(err, "Failed to set chat menu button")
		} else {
			logger.Info("Chat menu button set to webapp: %s", settings.WebAppURL)
		}
	}

	startListen(80, handler.HandleUpdate)
}

func startListen(port int, handleUpdate func(*telegram.Update) error) {
	logger.Info("Bot started")
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var update telegram.Update
		err := json.NewDecoder(r.Body).Decode(&update)

		if err != nil {
			logger.Error(err, "Failed to parse update from request")
			return
		}
		err = handleUpdate(&update)
		if err != nil {
			logger.Error(err, "Failed to handle update from request")
			return
		}
	})
	if err := http.ListenAndServe(":80", nil); err != nil {
		logger.Error(err, "Server failed")
		os.Exit(1)
	}
}
