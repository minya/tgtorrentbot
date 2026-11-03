package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/minya/logger"
	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/environment"
)

type DownloadByFileCommand struct {
	Doc *telegram.Document
	environment.Env
}

type DownloadByFileCommandFactory struct {
	environment.Env
}

func (factory *DownloadByFileCommandFactory) Accepts(upd *telegram.Update) (bool, Command) {
	if upd == nil || upd.Message == nil {
		return false, nil
	}
	if upd.Message.HasDocument() && isTorrentDocument(upd.Message.Document) {
		return true, &DownloadByFileCommand{
			Doc: upd.Message.Document,
			Env: factory.Env,
		}
	}
	return false, nil
}

// isTorrentDocument reports whether an uploaded document is a .torrent
// file, by name or MIME type. Other documents (a PDF, a photo sent as a
// file) fall through to the "Expected a command…" reply instead of being
// offered a category and then rejected by Transmission.
func isTorrentDocument(doc *telegram.Document) bool {
	return strings.HasSuffix(strings.ToLower(doc.FileName), ".torrent") ||
		doc.MimeType == "application/x-bittorrent"
}

func (cmd *DownloadByFileCommand) Handle(ctx context.Context, upd *telegram.Update) error {
	memFileID := cmd.FileIDLookup.Add(cmd.Doc.FileID)

	chatID := upd.Message.Chat.ID
	keyboard := buildCategoryKeyboard(memFileID)
	_, err := cmd.TgApi.SendMessage(ctx, telegram.SendMessageParams{
		ChatID:      chatID,
		Text:        "Select category:",
		ReplyMarkup: keyboard,
	})
	if err != nil {
		logger.Error(err, "Error sending category selection message")
		return err
	}
	return nil
}

func buildCategoryKeyboard(memFileID string) telegram.InlineKeyboardMarkup {
	var buttons [][]telegram.InlineKeyboardButton

	for _, cat := range AllCategories() {
		button := telegram.InlineKeyboardButton{
			Text:         cat.DisplayName(),
			CallbackData: fmt.Sprintf("/dlfilecat %s %s", cat.String(), memFileID),
		}
		buttons = append(buttons, []telegram.InlineKeyboardButton{button})
	}

	return telegram.InlineKeyboardMarkup{
		InlineKeyboard: buttons,
	}
}
