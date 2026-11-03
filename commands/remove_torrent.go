package commands

import (
	"context"
	"fmt"
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

func (cmd *RemoveTorrentCommand) Handle(ctx context.Context, upd *telegram.Update) error {
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
	chatID := upd.Message.Chat.ID
	// Never call RemoveTorrents with no torrents: the user would be told
	// "Removed." for a mistyped id, and an empty id list is one step from
	// a nil one, which Transmission reads as "all torrents".
	if len(torrents) == 0 {
		SendMessage(ctx, cmd.TgApi, telegram.SendMessageParams{
			Text:   fmt.Sprintf("No torrent with id %d.", cmd.TorrentID),
			ChatID: chatID,
		})
		return nil
	}
	err = cmd.TransmissionClient.RemoveTorrents(torrents, true)
	if err != nil {
		logger.Error(err, "Error removing torrents")
		return err
	}
	SendMessage(ctx, cmd.TgApi, telegram.SendMessageParams{
		Text:   "Removed.",
		ChatID: chatID,
	})
	return nil
}
