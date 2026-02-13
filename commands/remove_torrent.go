package commands

import (
	"regexp"
	"strconv"

	"github.com/minya/logger"
	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/environment"
	"github.com/odwrtw/transmission"
)

type RemoveTorrentCommand struct {
	TorrentID int
	environment.Env
}

type RemoveTorrentCommandFactory struct {
	environment.Env
}

var reRemCmd = regexp.MustCompile(`^/remove\s(\d+)\s*?$`)

func (factory *RemoveTorrentCommandFactory) Accepts(upd *telegram.Update) (bool, Command) {
	if upd == nil || upd.Message == nil {
		return false, nil
	}
	if found := reRemCmd.FindStringSubmatch(upd.Message.Text); len(found) == 2 {
		torrentID, err := strconv.Atoi(found[1])
		if err != nil {
			return false, nil
		}

		return true, &RemoveTorrentCommand{
			TorrentID: torrentID,
			Env:       factory.Env,
		}
	}
	return false, nil
}

func (cmd *RemoveTorrentCommand) Handle(upd *telegram.Update) error {
	allTorrents, err := cmd.TransmissionClient.GetTorrents()
	if err != nil {
		logger.Error(err, "Error getting torrents")
		return err
	}

	torrents := []*transmission.Torrent{}
	for _, torrent := range allTorrents {
		if torrent.ID == cmd.TorrentID {
			torrents = append(torrents, torrent)
		}
	}
	err = cmd.TransmissionClient.RemoveTorrents(torrents, true)
	if err != nil {
		logger.Error(err, "Error removing torrents")
		return err
	}
	chatID := upd.Message.Chat.Id
	cmd.TgApi.SendMessage(telegram.ReplyMessage{
		Text:   "Removed.",
		ChatId: chatID,
	})
	return nil
}
