package commands

import (
	"fmt"

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
	if upd.Message.HasDocument() {
		return true, &DownloadByFileCommand{
			Doc: upd.Message.Document,
			Env: factory.Env,
		}
	}
	return false, nil
}

func (cmd *DownloadByFileCommand) Handle(upd *telegram.Update) error {
	memFileID := cmd.FileIDLookup.Add(cmd.Doc.FileID)

	chatID := upd.Message.Chat.Id
	keyboard := buildCategoryKeyboard(memFileID)
	err := cmd.TgApi.SendMessage(telegram.ReplyMessage{
		ChatId:      chatID,
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
