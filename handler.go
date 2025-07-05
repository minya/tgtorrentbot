package main

import (
	"encoding/base64"
	"fmt"
	"sort"
	"strings"

	"github.com/minya/logger"
	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/rutracker"
	"github.com/odwrtw/transmission"
)

type UpdatesHandler struct {
	transmissionClient *transmission.Client
	tgApi              *telegram.Api
	downloadPath       string
	rutrackerConfig    *rutracker.Config
	notify             func()
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

	handler.notify()

	if upd.Message.HasDocument() {
		return handler.handleTorrentFile(&upd.Message.Document, replyChatID)
	}

	if messageText == "" {
		handler.tgApi.SendMessage(telegram.ReplyMessage{
			ChatId: replyChatID,
			Text:   "Ожидается команда или файл torrent", // TODO: translate
		})
		return nil
	}

	if messageText[0] == '/' {
		return handler.handleCommand(messageText, replyChatID)
	} else {
		return handler.handleSearchCommand(messageText, replyChatID)
	}
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

	return fmt.Errorf("Неизвестная команда") // TODO: translate
}

func (handler *UpdatesHandler) handleListCommand(replyChatID int) error {
	torrents, err := handler.transmissionClient.GetTorrents()
	if err != nil {
		logger.Error(err, "Error getting torrents")
		handler.tgApi.SendMessage(telegram.ReplyMessage{
			Text:   "Ошибка",
			ChatId: replyChatID,
		})
		return err
	}
	logger.Debug(fmt.Sprintf("Torrents received, count: %d", len(torrents)))

	if len(torrents) == 0 {
		handler.tgApi.SendMessage(telegram.ReplyMessage{
			Text:   "Нет активных торрентов", // TODO: translate
			ChatId: replyChatID,
		})
		return nil
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
		logger.Error(err, "Error getting torrents")
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
		logger.Error(err, "Error removing torrents")
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

	return handler.addTorrentAndReply(content, chatID)
}

func (handler *UpdatesHandler) handleDownloadCommand(downloadUrl string, chatID int) error {
	cfg := handler.rutrackerConfig
	rutrackerClient, err := rutracker.NewAuthenticatedRutrackerClient(cfg.Username, cfg.Password)
	if err != nil {
		logger.Error(err, "Error creating authenticated rutracker client")
		return err
	}
	torrentBytes, err := rutrackerClient.DownloadTorrent(downloadUrl)
	if err != nil {
		return err
	}

	torrentBase64 := base64.StdEncoding.EncodeToString(torrentBytes)
	logger.Debug(fmt.Sprintf("Torrent encoded as base64: %s", torrentBase64))

	return handler.addTorrentAndReply(torrentBytes, chatID)
}

func (handler *UpdatesHandler) handleSearchCommand(pattern string, chatID int) error {
	logger.Info("Starting search", "pattern", pattern)
	cfg := handler.rutrackerConfig
	rutrackerClient, err := rutracker.NewAuthenticatedRutrackerClient(cfg.Username, cfg.Password)
	if err != nil {
		logger.Error(err, "Error creating authenticated rutracker client")
		return err
	}
	found, err := rutrackerClient.Find(pattern)
	if err != nil {
		logger.Error(err, "Error searching")
		return err
	}

	logger.Info("found: %v results\n", len(found))
	logger.Debug("found: %v\n", found)

	if len(found) == 0 {
		handler.tgApi.SendMessage(telegram.ReplyMessage{
			ChatId: chatID,
			Text:   "Ничего не найдено", // TODO: translate
		})
		return nil
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
							Text:         "Добавить", // TODO: translate
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
		logger.Error(err, "Error from transmission RPC")
		return err
	}

	torrent.Labels = []string{fmt.Sprintf("%v", chatID)}
	err = torrent.Update()

	if err != nil {
		logger.Error(err, "Error updating torrent")
		return err
	}

	handler.tgApi.SendMessage(telegram.ReplyMessage{
		ChatId: chatID,
		Text:   fmt.Sprintf("Добавлено: %v", torrent.ID), // TODO: translate
	})
	return nil
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
