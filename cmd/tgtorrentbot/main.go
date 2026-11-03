package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/minya/logger"
	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/environment"
	"github.com/minya/tgtorrentbot/internal"
	"github.com/minya/tgtorrentbot/internal/graceful"
	"github.com/minya/tgtorrentbot/internal/transmissionrpc"
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

	transmissionClient, err := transmissionrpc.New(
		settings.TransmissionRPC.Address,
		settings.TransmissionRPC.User,
		settings.TransmissionRPC.Password,
	)
	if err != nil {
		logger.Fatal(err, "Can't create transmission client")
	}

	api := telegram.NewAPI(settings.BotToken)
	notify := CreateCompletedCheckRoutine(transmissionClient, api)
	var fileIDLookup internal.FileIDLookup

	env := environment.Env{
		TransmissionClient: transmissionClient,
		TgApi:              api,
		DownloadPath:       settings.DownloadPath,
		RutrackerConfig:    &settings.RutrackerConfig,
		WebAppURL:          settings.WebAppURL,
		AllowedUsers:       settings.AllowedUsers,
		FileIDLookup:       &fileIDLookup,
	}

	logger.Info("Access restricted to %d allowed user(s)", len(settings.AllowedUsers))

	handler := NewUpdatesHandler(env, notify)

	webhookSecret := settings.WebhookSecret
	if webhookSecret == "" {
		webhookSecret = deriveWebhookSecret(settings.BotToken)
	}
	webhookParams := telegram.SetWebhookParams{
		URL:         settings.WebHookURL,
		SecretToken: webhookSecret,
	}

	err = api.SetWebhook(context.Background(), webhookParams)
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
					URL: settings.WebAppURL,
				},
			},
		}
		err = api.SetChatMenuButton(context.Background(), menuButtonParams)
		if err != nil {
			logger.Error(err, "Failed to set chat menu button")
		} else {
			logger.Info("Chat menu button set to webapp: %s", settings.WebAppURL)
		}
	}

	startListen(80, webhookSecret, handler.HandleUpdate)
}

func startListen(port int, webhookSecret string, handleUpdate func(context.Context, *telegram.Update) error) {
	logger.Info("Bot started")
	var inFlight sync.WaitGroup
	http.Handle("/", newWebhookHandler(webhookSecret, handleUpdate, &inFlight))
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	// On SIGTERM, stop taking updates and let the ones being handled finish
	// (an add is two RPCs, AddTorrent then the labels). Updates Telegram
	// has not delivered yet are redelivered to the next instance.
	if err := graceful.Serve(srv, func(ctx context.Context) { waitGroup(ctx, &inFlight) }); err != nil {
		logger.Error(err, "Server failed")
		os.Exit(1)
	}
}

// waitGroup waits for wg or until ctx is done.
func waitGroup(ctx context.Context, wg *sync.WaitGroup) {
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		logger.Warn("Shutdown deadline passed with updates still being handled")
	}
}
