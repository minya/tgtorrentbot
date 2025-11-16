package commands

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"

	"github.com/minya/logger"
	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/environment"
	"github.com/minya/tgtorrentbot/rutracker"
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
	if upd.CallbackQuery.Data == "" {
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

func (cmd *DownloadCommand) Handle(chatID int) error {
	cfg := cmd.RutrackerConfig
	rutrackerClient, err := rutracker.NewAuthenticatedRutrackerClient(cfg.Username, cfg.Password)
	if err != nil {
		logger.Error(err, "Error creating authenticated rutracker client")
		return err
	}
	torrentBytes, err := rutrackerClient.DownloadTorrent(cmd.URL)
	if err != nil {
		return err
	}

	return cmd.addTorrentAndReply(torrentBytes, chatID)
}

func (cmd *DownloadCommand) addTorrentAndReply(content []byte, chatID int) error {
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
