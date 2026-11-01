package commands

import (
	"context"
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

func (cmd *expiredFileButtonCommand) Handle(ctx context.Context, upd *telegram.Update) error {
	AnswerCallbackQuery(ctx, upd, cmd.TgApi)
	chatID := upd.CallbackQuery.Message.Chat.ID
	logger.Warn("File ID lookup expired or missing for callback %q", upd.CallbackQuery.Data)
	_, err := cmd.TgApi.SendMessage(ctx, telegram.SendMessageParams{
		ChatID: chatID,
		Text:   "Кнопка устарела. Пожалуйста, загрузите файл заново.",
	})
	return err
}

func (cmd *DownloadFileWithCategoryCommand) Handle(ctx context.Context, upd *telegram.Update) error {
	api := cmd.TgApi
	AnswerCallbackQuery(ctx, upd, api)
	file, err := api.GetFile(ctx, cmd.FileID)
	chatID, userID := callbackOrigin(upd)
	if err != nil {
		logger.Error(err, "Error getting file")
		api.SendMessage(ctx, telegram.SendMessageParams{
			ChatID: chatID,
			Text:   "Error",
		})
		return Replied(err)
	}
	content, err := api.DownloadFile(ctx, file)
	if err != nil {
		logger.Error(err, "Error downloading file")
		api.SendMessage(ctx, telegram.SendMessageParams{
			ChatID: chatID,
			Text:   fmt.Sprintf("Download error: %v", err),
		})
		return Replied(err)
	}

	downloadCmd := &DownloadCommand{
		Env: cmd.Env,
	}

	return downloadCmd.addTorrentAndReply(ctx, content, chatID, userID, cmd.Category)
}
