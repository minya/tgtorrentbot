package commands

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"

	"github.com/minya/logger"
	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/environment"
	"github.com/odwrtw/transmission"
)

type DownloadCommand struct {
	URL string
	environment.Env
}

type DownloadCommandFactory struct {
	environment.Env
}

var reDownloadCmd = regexp.MustCompile(`^/dl\s(.+?)$`)

func (factory *DownloadCommandFactory) Accepts(upd *telegram.Update) (bool, Command) {
	if upd == nil || upd.CallbackQuery == nil {
		return false, nil
	}
	if found := reDownloadCmd.FindStringSubmatch(upd.CallbackQuery.Data); len(found) == 2 {
		url := strings.TrimSpace(found[1])
		return true, &DownloadCommand{
			URL: url,
			Env: factory.Env,
		}
	}
	return false, nil
}

func (cmd *DownloadCommand) Handle(upd *telegram.Update) error {
	AnswerCallbackQuery(upd, cmd.TgApi)

	keyboard := cmd.buildCategoryKeyboard()
	cmd.TgApi.SendMessage(telegram.ReplyMessage{
		ChatId:      upd.CallbackQuery.Message.Chat.Id,
		Text:        "Выберите категорию:", // TODO: translate
		ReplyMarkup: keyboard,
	})
	return nil
}

func (cmd *DownloadCommand) buildCategoryKeyboard() telegram.InlineKeyboardMarkup {
	var buttons [][]telegram.InlineKeyboardButton

	for _, cat := range AllCategories() {
		button := telegram.InlineKeyboardButton{
			Text:         cat.DisplayName(),
			CallbackData: fmt.Sprintf("/dlcat %s %s", cat.String(), cmd.URL),
		}
		buttons = append(buttons, []telegram.InlineKeyboardButton{button})
	}

	return telegram.InlineKeyboardMarkup{
		InlineKeyboard: buttons,
	}
}

func (cmd *DownloadCommand) addTorrentAndReply(content []byte, chatID int64, category Category) error {
	torrentBase64 := base64.StdEncoding.EncodeToString(content)

	downloadDir := fmt.Sprintf("%s/%s", cmd.DownloadPath, category.String())

	logger.Debug(fmt.Sprintf("Adding torrent with category %s to directory %s", category.String(), downloadDir))

	torrent, err := cmd.TransmissionClient.AddTorrent(transmission.AddTorrentArg{
		Metainfo:    torrentBase64,
		DownloadDir: downloadDir,
	})

	if err != nil {
		logger.Error(err, "Error from transmission RPC")
		return err
	}

	labels := []string{fmt.Sprintf("%v", chatID), category.String()}
	logger.Debug(fmt.Sprintf("Torrent added with ID %v, setting labels to %v", torrent.ID, labels))

	err = torrent.Set(transmission.SetTorrentArg{
		Labels: labels,
	})

	if err != nil {
		logger.Error(err, "Error setting torrent labels")
		return err
	}

	logger.Debug(fmt.Sprintf("Torrent %v labels set successfully", torrent.ID))

	cmd.TgApi.SendMessage(telegram.ReplyMessage{
		ChatId: chatID,
		Text:   fmt.Sprintf("Добавлено: %v [%s]", torrent.ID, category.DisplayName()), // TODO: translate
	})
	return nil
}
