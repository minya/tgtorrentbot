package commands

import (
	"encoding/base64"
	"fmt"

	"github.com/minya/logger"
	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/environment"
	"github.com/odwrtw/transmission"
)

type DownloadByFileCommand struct {
	URL string
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

func (cmd *DownloadByFileCommand) Handle(chatID int64) error {
	api := cmd.TgApi
	file, err := api.GetFile(cmd.Doc.FileID)
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

	return cmd.addTorrentAndReply(content, chatID)
}

func (cmd *DownloadByFileCommand) addTorrentAndReply(content []byte, chatID int64) error {
	torrentBase64 := base64.StdEncoding.EncodeToString(content)

	torrent, err := cmd.TransmissionClient.AddTorrent(transmission.AddTorrentArg{
		Metainfo: torrentBase64,
	})

	if err != nil {
		logger.Error(err, "Error from transmission RPC")
		return err
	}

	torrent.Labels = []string{fmt.Sprintf("%v", chatID)}
	err = torrent.Update()

	if err != nil {
		logger.Error(err, "Error updating torrent")
		return err
	}

	cmd.TgApi.SendMessage(telegram.ReplyMessage{
		ChatId: chatID,
		Text:   fmt.Sprintf("Добавлено: %v", torrent.ID), // TODO: translate
	})
	return nil
}
