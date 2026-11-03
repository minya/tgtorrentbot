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
		return true, &outdatedCategoryButtonCommand{Category: categoryStr, Env: factory.Env}
	}

	fileID, ok := factory.FileIDLookup.Get(memFileID)
	if !ok {
		return true, &expiredFileButtonCommand{Text: expiredFileText, Env: factory.Env}
	}

	return true, &DownloadFileWithCategoryCommand{
		FileID:   fileID,
		Category: category,
		Env:      factory.Env,
	}
}

// expiredFileButtonCommand answers a category button whose lookup entry is
// gone (the bot restarted, or the entry aged out). Text says what to send
// again, which differs between the file and the magnet flow.
type expiredFileButtonCommand struct {
	Text string
	environment.Env
}

const (
	expiredFileText   = "This button has expired. Please upload the .torrent file again."
	expiredMagnetText = "This button has expired. Please send the magnet link again."
)

func (cmd *expiredFileButtonCommand) Handle(ctx context.Context, upd *telegram.Update) error {
	AnswerCallbackQuery(ctx, upd, cmd.TgApi)
	logger.Warn("Lookup entry expired or missing for callback %q", upd.CallbackQuery.Data)
	return SendMessage(ctx, cmd.TgApi, telegram.SendMessageParams{
		ChatID: upd.CallbackQuery.Message.Chat.ID,
		Text:   cmd.Text,
	})
}

func (cmd *DownloadFileWithCategoryCommand) Handle(ctx context.Context, upd *telegram.Update) error {
	api := cmd.TgApi
	AnswerCallbackQuery(ctx, upd, api)
	file, err := api.GetFile(ctx, cmd.FileID)
	chatID, userID := callbackOrigin(upd)
	if err != nil {
		logger.Error(err, "Error getting file")
		SendMessage(ctx, api, telegram.SendMessageParams{
			ChatID: chatID,
			Text:   "Error",
		})
		return Replied(err)
	}
	content, err := api.DownloadFile(ctx, file)
	if err != nil {
		logger.Error(err, "Error downloading file")
		SendMessage(ctx, api, telegram.SendMessageParams{
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
