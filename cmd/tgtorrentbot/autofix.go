package main

import (
	"context"
	"fmt"

	"github.com/minya/logger"
	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/commands"
	"github.com/minya/tgtorrentbot/internal/autofix"
	"github.com/minya/tgtorrentbot/internal/mediatools"
	"github.com/odwrtw/transmission"
)

// startAutofix starts the auto-fixer when an Anthropic API key is set. It
// returns the hook for the completion checker (nil when the fixer is off)
// and a drain that waits for the run in progress at shutdown.
func startAutofix(ctx context.Context, settings Settings, trans mediatools.TorrentClient, api *telegram.API) (completedFunc, func(context.Context), error) {
	cfg := settings.Autofix
	if cfg.AnthropicAPIKey == "" {
		return nil, func(context.Context) {}, nil
	}
	dl, inc, err := mediatools.ResolveRoots(settings.DownloadPath, cfg.IncompletePath)
	if err != nil {
		return nil, nil, fmt.Errorf("download path: %w", err)
	}
	tools := mediatools.New(mediatools.Config{
		DownloadPath:            dl,
		IncompletePath:          inc,
		JellyfinURL:             cfg.JellyfinURL,
		JellyfinAPIKey:          cfg.JellyfinAPIKey,
		AudiobookshelfURL:       cfg.AudiobookshelfURL,
		AudiobookshelfAPIKey:    cfg.AudiobookshelfAPIKey,
		AudiobookshelfLibraryID: cfg.AudiobookshelfLibraryID,
	}, trans)
	agent, err := autofix.NewAgent(ctx, tools, cfg.AnthropicAPIKey, cfg.Model)
	if err != nil {
		return nil, nil, err
	}
	fixer := autofix.NewFixer(agent, func(ctx context.Context, item *autofix.Item, text string) error {
		params := telegram.SendMessageParams{ChatID: item.ChatID, Text: text}
		if item.ReplyTo != 0 {
			params.ReplyParameters = &telegram.ReplyParameters{MessageID: item.ReplyTo, AllowSendingWithoutReply: true}
		}
		return commands.SendMessage(ctx, api, params)
	})
	drain := fixer.Start(ctx)
	logger.Info("[Autofix] enabled, model %s, downloads at %s", agent.Model(), dl)
	return func(t *transmission.Torrent, chatID, messageID int64) {
		fixer.Submit(t, chatID, messageID)
	}, drain, nil
}
