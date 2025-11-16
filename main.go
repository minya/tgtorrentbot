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
	logLevel := flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	prettyLog := flag.Bool("pretty-log", true, "Enable pretty logging")
	flag.Parse()

	logger.InitLogger(logger.Config{
		Level:      *logLevel,
		Pretty:     *prettyLog,
		WithCaller: true,
	})

	settings, err := ReadSettings(*settingsPath)
	if err != nil {
		logger.Fatal(err, "Failed to read settings")
	}

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
	updateRoutine, chanNotify := CreateCompletedCheckRoutine(transmissionClient, &api)
	go updateRoutine()

	env := environment.Env{
		TransmissionClient: transmissionClient,
		TgApi:              &api,
		DownloadPath:       settings.DownloadPath,
		RutrackerConfig:    &settings.RutrackerConfig,
	}

	notify := func() {
		select {
		case chanNotify <- 1:
		default:
		}
	}

	handler := NewUpdatesHandler(env, notify)

	logger.Info("Bot started")
	err = telegram.StartPolling(&api, handler.HandleUpdate, 3*time.Second, -1)
	if err != nil {
		logger.Fatal(err, "Failed to start polling")
	}
}
