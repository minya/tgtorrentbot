package commands

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/minya/logger"
	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/environment"
	"github.com/minya/tgtorrentbot/rutracker"
)

var reSearchCmd = regexp.MustCompile(`^/search\s(.+?)$`)

type SearchCommand struct {
	Pattern string
	environment.Env
}

type SearchCommandFactory struct {
	environment.Env
}

func (factory *SearchCommandFactory) Accepts(upd *telegram.Update) (bool, Command) {
	if upd == nil || upd.Message == nil {
		return false, nil
	}
	if cmd := Match(upd.Message.Text); cmd != nil {
		cmd.Env = factory.Env
		return true, cmd
	}
	return false, nil
}

func Match(cmdText string) *SearchCommand {
	if cmdText == "" {
		return nil
	}
	if found := reSearchCmd.FindStringSubmatch(cmdText); len(found) == 2 {
		pattern := strings.TrimSpace(found[1])
		return &SearchCommand{Pattern: pattern}
	}
	if cmdText[0] != '/' {
		pattern := strings.TrimSpace(cmdText)
		return &SearchCommand{Pattern: pattern}
	}
	return nil
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func (cmd *SearchCommand) Handle(chatID int64) error {
	logger.Info("Starting search, pattern: %s", cmd.Pattern)
	cfg := cmd.RutrackerConfig
	rutrackerClient, err := rutracker.NewAuthenticatedRutrackerClient(cfg.Username, cfg.Password)
	if err != nil {
		logger.Error(err, "Error creating authenticated rutracker client")
		return err
	}
	found, err := rutrackerClient.Find(cmd.Pattern)
	if err != nil {
		logger.Error(err, "Error searching")
		return err
	}

	logger.Info("found: %v results\n", len(found))
	logger.Debug("found: %v\n", found)

	if len(found) == 0 {
		cmd.TgApi.SendMessage(telegram.ReplyMessage{
			ChatId: chatID,
			Text:   "Ничего не найдено", // TODO: translate
		})
		return nil
	}

	sort.Slice(found, func(i, j int) bool {
		return found[i].Seeders > found[j].Seeders
	})

	for _, f := range found[:min(10, len(found))] {
		cmd.TgApi.SendMessage(telegram.ReplyMessage{
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
