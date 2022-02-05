package main

import (
	"encoding/base64"
	"fmt"
	"log"

	"github.com/minya/telegram"
	"github.com/odwrtw/transmission"
)

type UpdatesHandler struct {
	transmissionClient *transmission.Client
	tgApi              *telegram.Api
	downloadPath       string
}

func (handler *UpdatesHandler) HandleUpdate(upd *telegram.Update) error {
	if upd.Message.Text != "" && upd.Message.Text[0] == '/' {
		return handler.handleCommand(upd.Message.Text, upd.Message.Chat.Id)
	}
	if !upd.Message.HasDocument() {
		handler.tgApi.SendMessage(telegram.ReplyMessage{
			ChatId: upd.Message.Chat.Id,
			Text:   "Ожидается команда или файл torrent",
		})
		return nil
	}
	return handler.handleTorrentFile(&upd.Message.Document, upd.Message.Chat.Id)
}

func (handler *UpdatesHandler) handleCommand(commandText string, replyChatID int) error {
	_, cmd := ParseCommand(commandText)
	if _, ok := cmd.(ListCommand); ok {
		return handler.handleListCommand(replyChatID)
	}

	if removeCmd, ok := cmd.(RemoveTorrentCommand); ok {
		return handler.handleRemoveTorrent(removeCmd.TorrentID, replyChatID)
	}
	return fmt.Errorf("неизвестная команда")
}

func (handler *UpdatesHandler) handleListCommand(replyChatID int) error {
	torrents, err := handler.transmissionClient.GetTorrents()
	if err != nil {
		return err
	}

	for _, torrent := range torrents {
		msg := fmt.Sprintf("%v. %v %v\n", torrent.ID, torrent.Name, torrent.PercentDone*100)
		handler.tgApi.SendMessage(telegram.ReplyMessage{
			Text:   msg,
			ChatId: replyChatID,
			ReplyMarkup: telegram.InlineKeyboardMarkup{
				InlineKeyboard: [][]telegram.InlineKeyboardButton{
					{
						telegram.InlineKeyboardButton{
							Text:         "🚀",
							Url:          "/remove",
							CallbackData: fmt.Sprintf("/remove %v", torrent.ID),
						},
					},
				},
			},
		})
	}
	return nil
}

func (handler *UpdatesHandler) handleRemoveTorrent(torrentID int, replyChatID int) error {
	allTorrents, err := handler.transmissionClient.GetTorrents()
	if err != nil {
		return err
	}

	torrents := []*transmission.Torrent{}
	for _, torrent := range allTorrents {
		if torrent.ID == torrentID {
			torrents = append(torrents, torrent)
		}
	}
	err = handler.transmissionClient.RemoveTorrents(torrents, true)
	if err != nil {
		return err
	}
	handler.tgApi.SendMessage(telegram.ReplyMessage{
		Text:   "Удалено.",
		ChatId: replyChatID,
	})
	return nil
}

func (handler *UpdatesHandler) handleTorrentFile(doc *telegram.Document, chatID int) error {
	api := handler.tgApi
	file, err := api.GetFile(doc.FileID)
	if err != nil {
		api.SendMessage(telegram.ReplyMessage{
			ChatId: chatID,
			Text:   "Ошибка",
		})
		return err
	}
	content, err := api.DownloadFile(file)
	if err != nil {
		api.SendMessage(telegram.ReplyMessage{
			ChatId: chatID,
			Text:   fmt.Sprintf("Ошибка загрузки %v", err),
		})
		return err
	}

	torrentBase64 := base64.StdEncoding.EncodeToString(content)

	torrent, err := handler.transmissionClient.AddTorrent(transmission.AddTorrentArg{
		Metainfo: torrentBase64,
	})
	if err != nil {
		log.Printf("[ERROR] Error from transmission rpc. %v\n", err)
		return err
	}
	if err != nil {
		log.Printf("[ERROR] Error from transmission rpc. %v\n", err)
		return err
	}

	api.SendMessage(telegram.ReplyMessage{
		ChatId: chatID,
		Text:   fmt.Sprintf("Добавлено: %v", torrent.ID),
	})
	return nil
}
