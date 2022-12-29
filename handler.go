package main

import (
	"encoding/base64"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/rutracker"
	"github.com/odwrtw/transmission"
)

type UpdatesHandler struct {
	transmissionClient *transmission.Client
	tgApi              *telegram.Api
	downloadPath       string
	rutrackerConfig    *rutracker.Config
}

func (handler *UpdatesHandler) HandleUpdate(upd *telegram.Update) error {
	var messageText string
	var replyChatID int
	if upd.Message.MessageId != 0 {
		messageText = upd.Message.Text
		replyChatID = upd.Message.Chat.Id
	} else {
		messageText = upd.CallbackQuery.Data
		replyChatID = upd.CallbackQuery.Message.Chat.Id
	}

	if messageText != "" && messageText[0] == '/' {
		return handler.handleCommand(messageText, replyChatID)
	}

	if !upd.Message.HasDocument() {
		handler.tgApi.SendMessage(telegram.ReplyMessage{
			ChatId: replyChatID,
			Text:   "Ожидается команда или файл torrent",
		})
		return nil
	}
	return handler.handleTorrentFile(&upd.Message.Document, replyChatID)
}

func (handler *UpdatesHandler) handleCommand(commandText string, replyChatID int) error {
	_, cmd := ParseCommand(commandText)
	if _, ok := cmd.(ListCommand); ok {
		return handler.handleListCommand(replyChatID)
	}

	if removeCmd, ok := cmd.(RemoveTorrentCommand); ok {
		return handler.handleRemoveTorrent(removeCmd.TorrentID, replyChatID)
	}

	if searchCmd, ok := cmd.(SearchCommand); ok {
		return handler.handleSearchCommand(searchCmd.Pattern, replyChatID)
	}

	if downloadCmd, ok := cmd.(DownloadCommand); ok {
		return handler.handleDownloadCommand(downloadCmd.URL, replyChatID)
	}

	return fmt.Errorf("неизвестная команда")
}

func (handler *UpdatesHandler) handleListCommand(replyChatID int) error {
	torrents, err := handler.transmissionClient.GetTorrents()
	if err != nil {
		return err
	}
	var b strings.Builder
	for _, torrent := range torrents {
		fmt.Fprintf(&b, "%v %v %v\n", torrent.ID, torrent.Name, torrent.PercentDone*100)
	}
	handler.tgApi.SendMessage(telegram.ReplyMessage{
		Text:   b.String(),
		ChatId: replyChatID,
	})
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

	return handler.addTorrentAndReply(content, chatID)
}

func (handler *UpdatesHandler) handleDownloadCommand(downloadUrl string, chatID int) error {
	cfg := handler.rutrackerConfig
	rutrackerClient, err := rutracker.NewAuthenticatedRutrackerClient(cfg.Username, cfg.Password)
	if err != nil {
		return err
	}
	torrentBytes, err := rutrackerClient.DownloadTorrent(downloadUrl)
	if err != nil {
		return err
	}

	torrentBase64 := base64.StdEncoding.EncodeToString(torrentBytes)
	log.Printf("torrentBase64: %v\n", torrentBase64)

	return handler.addTorrentAndReply(torrentBytes, chatID)
}

func (handler *UpdatesHandler) handleSearchCommand(pattern string, chatID int) error {
	log.Printf("Begin handle search: %v\n", pattern)
	cfg := handler.rutrackerConfig
	rutrackerClient, err := rutracker.NewAuthenticatedRutrackerClient(cfg.Username, cfg.Password)
	if err != nil {
		return err
	}
	found, err := rutrackerClient.Find(pattern)
	fmt.Printf("found: %v\n", found)
	if err != nil {
		return err
	}

	sort.Slice(found, func(i, j int) bool {
		return found[i].Seeders > found[j].Seeders
	})

	for _, f := range found[:min(10, len(found))] {
		handler.tgApi.SendMessage(telegram.ReplyMessage{
			ChatId: chatID,
			Text:   fmt.Sprintf("%v\r\n\r\nSize:%v%v\r\nSeeders: %v", f.Title, f.Size.Size, f.Size.Unit, f.Seeders),
			ReplyMarkup: &telegram.InlineKeyboardMarkup{
				InlineKeyboard: [][]telegram.InlineKeyboardButton{
					{
						{
							Text:         "Добавить",
							CallbackData: fmt.Sprintf("/dl %v", f.DownloadURL),
						},
					},
				},
			},
		})
	}
	return nil
}

func (handler *UpdatesHandler) addTorrentAndReply(content []byte, chatID int) error {
	torrentBase64 := base64.StdEncoding.EncodeToString(content)

	torrent, err := handler.transmissionClient.AddTorrent(transmission.AddTorrentArg{
		Metainfo: torrentBase64,
	})
	if err != nil {
		log.Printf("[ERROR] Error from transmission rpc. %v\n", err)
		return err
	}

	handler.tgApi.SendMessage(telegram.ReplyMessage{
		ChatId: chatID,
		Text:   fmt.Sprintf("Добавлено: %v", torrent.ID),
	})
	return nil
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
