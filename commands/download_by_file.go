package commands

import (
	"fmt"

	"github.com/minya/logger"
	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/environment"
)

type DownloadByFileCommand struct {
	URL     string
	Doc     *telegram.Document
	Content []byte // Store downloaded content for later use
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
	api := cmd.TgApi
	file, err := api.GetFile(cmd.Doc.FileID)
	chatID := upd.Message.Chat.Id
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

	// Store content and show category selection
	cmd.Content = content
	keyboard := cmd.buildCategoryKeyboard()
	cmd.TgApi.SendMessage(telegram.ReplyMessage{
		ChatId:      chatID,
		Text:        "Select category:",
		ReplyMarkup: keyboard,
	})
	return nil
}

func (cmd *DownloadByFileCommand) buildCategoryKeyboard() telegram.InlineKeyboardMarkup {
	var buttons [][]telegram.InlineKeyboardButton

	for _, cat := range AllCategories() {
		button := telegram.InlineKeyboardButton{
			Text:         cat.DisplayName(),
			CallbackData: fmt.Sprintf("/dlfilecat %s %s", cat.String(), cmd.Doc.FileID),
		}
		buttons = append(buttons, []telegram.InlineKeyboardButton{button})
	}

	return telegram.InlineKeyboardMarkup{
		InlineKeyboard: buttons,
	}
}

//func (cmd *DownloadByFileCommand) addTorrentAndReply(content []byte, chatID int64, category Category) error {
	//torrentBase64 := base64.StdEncoding.EncodeToString(content)

	//downloadDir := fmt.Sprintf("%s/%s", cmd.DownloadPath, category.String())

	//torrent, err := cmd.TransmissionClient.AddTorrent(transmission.AddTorrentArg{
		//Metainfo:    torrentBase64,
		//DownloadDir: downloadDir,
	//})

	//if err != nil {
		//logger.Error(err, "Error from transmission RPC")
		//return err
	//}

	//labels := []string{fmt.Sprintf("%v", chatID), category.String()}
	//err = torrent.Set(transmission.SetTorrentArg{
		//Labels: labels,
	//})

	//if err != nil {
		//logger.Error(err, "Error setting torrent labels")
		//return err
	//}

	//cmd.TgApi.SendMessage(telegram.ReplyMessage{
		//ChatId: chatID,
		//Text:   fmt.Sprintf("Добавлено: %v [%s]", torrent.ID, category.DisplayName()), // TODO: translate
	//})
	//return nil
//}
