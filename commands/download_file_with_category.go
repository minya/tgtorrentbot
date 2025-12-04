package commands

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/minya/logger"
	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/environment"
)

type DownloadFileWithCategoryCommand struct {
	FileID   string
	Category Category
	environment.Env
}

type DownloadFileWithCategoryCommandFactory struct {
	environment.Env
}

var reDownloadFileWithCategoryCmd = regexp.MustCompile(`^/dlfilecat\s+(\S+)\s+(.+?)$`)

func (factory *DownloadFileWithCategoryCommandFactory) Accepts(upd *telegram.Update) (bool, Command) {
	if upd == nil || upd.CallbackQuery == nil {
		return false, nil
	}
	if found := reDownloadFileWithCategoryCmd.FindStringSubmatch(upd.CallbackQuery.Data); len(found) == 3 {
		categoryStr := strings.TrimSpace(found[1])
		fileID := strings.TrimSpace(found[2])

		category, ok := ParseCategory(categoryStr)
		if !ok {
			logger.Error(nil, fmt.Sprintf("Invalid category: %s", categoryStr))
			return false, nil
		}

		return true, &DownloadFileWithCategoryCommand{
			FileID:   fileID,
			Category: category,
			Env:      factory.Env,
		}
	}
	return false, nil
}

func (cmd *DownloadFileWithCategoryCommand) Handle(chatID int64) error {
	api := cmd.TgApi
	file, err := api.GetFile(cmd.FileID)
	if err != nil {
		logger.Error(err, "Error getting file")
		api.SendMessage(telegram.ReplyMessage{
			ChatId: chatID,
			Text:   "Ошибка", // TODO: translate
		})
		return err
	}
	content, err := api.DownloadFile(file)
	if err != nil {
		logger.Error(err, "Error downloading file")
		api.SendMessage(telegram.ReplyMessage{
			ChatId: chatID,
			Text:   fmt.Sprintf("Ошибка загрузки %v", err), // TODO: translate
		})
		return err
	}

	// Reuse the addTorrentAndReply method from DownloadByFileCommand
	downloadCmd := &DownloadByFileCommand{
		Env: cmd.Env,
	}

	return downloadCmd.addTorrentAndReply(content, chatID, cmd.Category)
}
