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

func (cmd *DownloadFileWithCategoryCommand) Handle(upd *telegram.Update) error {
	api := cmd.TgApi
	AnswerCallbackQuery(upd, api)
	file, err := api.GetFile(cmd.FileID)
	chatID := upd.CallbackQuery.Message.Chat.Id
	if err != nil {
		logger.Error(err, "Error getting file")
		api.SendMessage(telegram.ReplyMessage{
			ChatId: chatID,
			Text:   "Error",
		})
		return err
	}
	content, err := api.DownloadFile(file)
	if err != nil {
		logger.Error(err, "Error downloading file")
		api.SendMessage(telegram.ReplyMessage{
			ChatId: chatID,
			Text:   fmt.Sprintf("Download error: %v", err),
		})
		return err
	}

	// Reuse the addTorrentAndReply method from DownloadByFileCommand
	downloadCmd := &DownloadCommand{
		Env: cmd.Env,
	}

	return downloadCmd.addTorrentAndReply(content, chatID, cmd.Category)
}
