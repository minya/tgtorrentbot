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
		fmt.Fprintf(&b, "%v %v %v\n", torrent.ID, torrent.Name, torrent.PercentDone*100)
	}
	cmd.TgApi.SendMessage(telegram.ReplyMessage{
		Text:   b.String(),
		ChatId: chatID,
	})

	return nil
}
