package commands

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/minya/logger"
	"github.com/minya/rutracker"
	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/environment"
	"github.com/minya/tgtorrentbot/internal"
)

type DownloadWithCategoryCommand struct {
	URL      string
	Category Category
	environment.Env
}

type DownloadWithCategoryCommandFactory struct {
	environment.Env
}

var reDownloadWithCategoryCmd = regexp.MustCompile(`^/dlcat\s+(\S+)\s+(.+?)$`)

func (factory *DownloadWithCategoryCommandFactory) Accepts(upd *telegram.Update) (bool, Command) {
	if upd == nil || upd.CallbackQuery == nil {
		return false, nil
	}
	if found := reDownloadWithCategoryCmd.FindStringSubmatch(upd.CallbackQuery.Data); len(found) == 3 {
		categoryStr := strings.TrimSpace(found[1])
		url := strings.TrimSpace(found[2])

		category, ok := ParseCategory(categoryStr)
		if !ok {
			return true, &outdatedCategoryButtonCommand{Category: categoryStr, Env: factory.Env}
		}

		return true, &DownloadWithCategoryCommand{
			URL:      url,
			Category: category,
			Env:      factory.Env,
		}
	}
	return false, nil
}

func (cmd *DownloadWithCategoryCommand) Handle(ctx context.Context, upd *telegram.Update) error {
	AnswerCallbackQuery(ctx, upd, cmd.TgApi)
	cfg := cmd.RutrackerConfig
	rutrackerClient, err := rutracker.NewAuthenticatedRutrackerClient(cfg.Username, cfg.Password, rutracker.WithTimeout(30*time.Second))
	if err != nil {
		logger.Error(err, "Error creating authenticated rutracker client")
		return err
	}
	torrentBytes, err := rutrackerClient.DownloadTorrent(cmd.URL)
	if err != nil {
		return err
	}
	if err := internal.CheckTorrentFile(torrentBytes); err != nil {
		chatID, _ := callbackOrigin(upd)
		SendMessage(ctx, cmd.TgApi, telegram.SendMessageParams{
			ChatID: chatID,
			Text:   "Rutracker returned a web page instead of the .torrent file (the login or the topic may have failed). Please try again.",
		})
		return Replied(fmt.Errorf("download %s: %w", cmd.URL, err))
	}

	// Reuse the addTorrentAndReply method from DownloadCommand
	downloadCmd := &DownloadCommand{
		URL: cmd.URL,
		Env: cmd.Env,
	}

	chatID, userID := callbackOrigin(upd)
	return downloadCmd.addTorrentAndReply(ctx, torrentBytes, chatID, userID, cmd.Category)
}
