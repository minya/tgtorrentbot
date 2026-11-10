package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/minya/logger"
	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/environment"
	"github.com/minya/tgtorrentbot/internal"
	"github.com/minya/tgtorrentbot/internal/graceful"
	"github.com/minya/tgtorrentbot/internal/rutrackersession"
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

	// Done at SIGTERM/SIGINT: stops the completion checker and the server.
	ctx := graceful.SignalContext()

	api := telegram.NewAPI(settings.BotToken)
	notify, checkerDone := CreateCompletedCheckRoutine(ctx, transmissionClient, api)
	var fileIDLookup internal.FileIDLookup

	env := environment.Env{
		TransmissionClient: transmissionClient,
		TgApi:              api,
		DownloadPath:       settings.DownloadPath,
		RutrackerConfig:    &settings.RutrackerConfig,
		Rutracker:          rutrackersession.New(settings.RutrackerConfig.Username, settings.RutrackerConfig.Password),
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

	startListen(ctx, 80, webhookSecret, handler, api, checkerDone)
}

func startListen(ctx context.Context, port int, webhookSecret string, handler *UpdatesHandler, api *telegram.API, checkerDone <-chan struct{}) {
	logger.Info("Bot started")
	webhook := newWebhookHandler(webhookSecret, handler.HandleUpdate)
	http.Handle("/", webhook)
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	// On SIGTERM, stop taking updates and let the ones being handled (an
	// add is two RPCs, AddTorrent then the labels) and a completion notice
	// being sent finish. Telegram never redelivers an update the webhook
	// acknowledged, so the chats of updates still running near the deadline
	// are told to send them again.
	if err := graceful.Serve(ctx, srv, func(ctx context.Context) {
		drain(ctx, webhook, checkerDone, &handler.inFlight, api)
	}); err != nil {
		logger.Error(err, "Server failed")
		os.Exit(1)
	}
}

// drain waits for the webhook's updates and the completion checker until
// noticeBudget before ctx's deadline, then tells the chats of the updates
// still being handled that they were interrupted.
func drain(ctx context.Context, webhook interface{ Wait(context.Context) error }, checkerDone <-chan struct{}, inFlight *inFlightChats, api *telegram.API) {
	waitCtx := ctx
	if deadline, ok := ctx.Deadline(); ok {
		var cancel context.CancelFunc
		waitCtx, cancel = context.WithDeadline(ctx, deadline.Add(-noticeBudget))
		defer cancel()
	}
	if err := webhook.Wait(waitCtx); err != nil {
		tellInterrupted(ctx, api, inFlight.list())
	}
	select {
	case <-checkerDone:
	case <-ctx.Done():
		logger.Warn("Shutdown deadline passed with a completion check still running")
	}
}
