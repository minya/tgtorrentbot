package commands

import (
	"regexp"
	"strings"
	"time"

	"github.com/minya/logger"
	"github.com/minya/rutracker"
	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/environment"
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
			logger.Error(nil, "Invalid category: %s", categoryStr)
			return false, nil
		}

		return true, &DownloadWithCategoryCommand{
			URL:      url,
			Category: category,
			Env:      factory.Env,
		}
	}
	return false, nil
}

func (cmd *DownloadWithCategoryCommand) Handle(upd *telegram.Update) error {
	AnswerCallbackQuery(upd, cmd.TgApi)
	cfg := cmd.RutrackerConfig
	rutrackerClient, err := rutracker.NewAuthenticatedRutrackerClient(cfg.Username, cfg.Password, rutracker.WithTimeout(30*time.Second), rutracker.WithIPv6())
	if err != nil {
		logger.Error(err, "Error creating authenticated rutracker client")
		return err
	}
	torrentBytes, err := rutrackerClient.DownloadTorrent(cmd.URL)
	if err != nil {
		return err
	}

	// Reuse the addTorrentAndReply method from DownloadCommand
	downloadCmd := &DownloadCommand{
		URL: cmd.URL,
		Env: cmd.Env,
	}

	chatID := upd.CallbackQuery.Message.Chat.Id
	return downloadCmd.addTorrentAndReply(torrentBytes, chatID, cmd.Category)
}
