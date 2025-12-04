package main

import (
	"flag"
	"time"

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
	}

	handler := NewUpdatesHandler(env, notify)

	logger.Info("Bot started")
	err = telegram.StartPolling(&api, handler.HandleUpdate, 3*time.Second, -1)
	if err != nil {
		logger.Fatal(err, "Failed to start polling")
	}
}
