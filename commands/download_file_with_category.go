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
	found := reDownloadFileWithCategoryCmd.FindStringSubmatch(upd.CallbackQuery.Data)
	if len(found) != 3 {
		return false, nil
	}
	categoryStr := strings.TrimSpace(found[1])
	memFileID := strings.TrimSpace(found[2])

	category, ok := ParseCategory(categoryStr)
	if !ok {
		return false, nil
	}

	fileID, ok := factory.FileIDLookup.Get(memFileID)
	if !ok {
		return true, &expiredFileButtonCommand{Env: factory.Env}
	}

	return true, &DownloadFileWithCategoryCommand{
		FileID:   fileID,
		Category: category,
		Env:      factory.Env,
	}
}

type expiredFileButtonCommand struct {
	environment.Env
}

func (cmd *expiredFileButtonCommand) Handle(upd *telegram.Update) error {
	AnswerCallbackQuery(upd, cmd.TgApi)
	chatID := upd.CallbackQuery.Message.Chat.Id
	logger.Warn("File ID lookup expired or missing for callback %q", upd.CallbackQuery.Data)
	return cmd.TgApi.SendMessage(telegram.ReplyMessage{
		ChatId: chatID,
		Text:   "Кнопка устарела. Пожалуйста, загрузите файл заново.",
	})
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

	downloadCmd := &DownloadCommand{
		Env: cmd.Env,
	}

	return downloadCmd.addTorrentAndReply(content, chatID, cmd.Category)
}
