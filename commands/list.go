package commands

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/minya/logger"
	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/environment"
)

type ListCommand struct {
	environment.Env
}

type ListCommandFactory struct {
	environment.Env
}

func (f *ListCommandFactory) Accepts(upd *telegram.Update) (bool, Command) {
	if upd == nil || upd.Message == nil {
		return false, nil
	}
	reListCmd := regexp.MustCompile(`^/list\s*?$`)
	if matched := reListCmd.Match([]byte(upd.Message.Text)); matched {
		return true, &ListCommand{f.Env}
	}
	return false, nil
}

func (cmd *ListCommand) Handle(chatID int64) error {
	torrents, err := cmd.TransmissionClient.GetTorrents()
	if err != nil {
		logger.Error(err, "Error getting torrents")
		cmd.TgApi.SendMessage(telegram.ReplyMessage{
			Text:   "Ошибка",
			ChatId: chatID,
		})
		return err
	}
	logger.Debug(fmt.Sprintf("Torrents received, count: %d", len(torrents)))

	if len(torrents) == 0 {
		cmd.TgApi.SendMessage(telegram.ReplyMessage{
			Text:   "Нет активных торрентов", // TODO: translate
			ChatId: chatID,
		})
		return nil
	}

	var b strings.Builder
	for _, torrent := range torrents {
		categoryLabel := "Unknown"
		logger.Debug(fmt.Sprintf("Torrent %v has %d labels: %v", torrent.ID, len(torrent.Labels), torrent.Labels))
		if len(torrent.Labels) >= 2 {
			// labels[0] is chatID, labels[1] is category
			if cat, ok := ParseCategory(torrent.Labels[1]); ok {
				categoryLabel = cat.DisplayName()
			} else {
				logger.Debug(fmt.Sprintf("Failed to parse category from label: %s", torrent.Labels[1]))
			}
		}
		fmt.Fprintf(&b, "[%s] %v %v %.0f%%\n", categoryLabel, torrent.ID, torrent.Name, torrent.PercentDone*100)
	}
	cmd.TgApi.SendMessage(telegram.ReplyMessage{
		Text:   b.String(),
		ChatId: chatID,
	})

	return nil
}
