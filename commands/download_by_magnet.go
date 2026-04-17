package commands

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/minya/logger"
	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/environment"
)

var reMagnetURI = regexp.MustCompile(`(?i)^magnet:\?`)

type DownloadByMagnetCommand struct {
	MagnetURI string
	environment.Env
}

type DownloadByMagnetCommandFactory struct {
	environment.Env
}

func (factory *DownloadByMagnetCommandFactory) Accepts(upd *telegram.Update) (bool, Command) {
	if upd == nil || upd.Message == nil {
		return false, nil
	}
	text := strings.TrimSpace(upd.Message.Text)
	if text == "" || !reMagnetURI.MatchString(text) {
		return false, nil
	}
	return true, &DownloadByMagnetCommand{
		MagnetURI: text,
		Env:       factory.Env,
	}
}

func (cmd *DownloadByMagnetCommand) Handle(upd *telegram.Update) error {
	memID := cmd.FileIDLookup.Add(cmd.MagnetURI)

	chatID := upd.Message.Chat.Id
	keyboard := buildMagnetCategoryKeyboard(memID)
	if err := cmd.TgApi.SendMessage(telegram.ReplyMessage{
		ChatId:      chatID,
		Text:        "Select category:",
		ReplyMarkup: keyboard,
	}); err != nil {
		logger.Error(err, "Error sending magnet category selection message")
		return err
	}
	return nil
}

func buildMagnetCategoryKeyboard(memID string) telegram.InlineKeyboardMarkup {
	var buttons [][]telegram.InlineKeyboardButton
	for _, cat := range AllCategories() {
		button := telegram.InlineKeyboardButton{
			Text:         cat.DisplayName(),
			CallbackData: fmt.Sprintf("/dlmagnet %s %s", cat.String(), memID),
		}
		buttons = append(buttons, []telegram.InlineKeyboardButton{button})
	}
	return telegram.InlineKeyboardMarkup{InlineKeyboard: buttons}
}
