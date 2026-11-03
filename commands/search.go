package commands

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/minya/logger"
	"github.com/minya/rutracker"
	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/environment"
)

// reSearchCmd matches "/search", optionally followed by a query that may
// span several lines ((?s) lets . match newlines).
var reSearchCmd = regexp.MustCompile(`(?s)^/search(?:\s+(.*))?$`)

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

// Match returns the search a message asks for, or nil. "/search <query>"
// and plain text (anything not starting with "/") are searches; a bare
// "/search" yields an empty Pattern, which Handle answers with a usage hint.
// Whitespace, newlines included, is collapsed to single spaces.
func Match(cmdText string) *SearchCommand {
	text := strings.TrimSpace(cmdText)
	if text == "" {
		return nil
	}
	if found := reSearchCmd.FindStringSubmatch(text); found != nil {
		return &SearchCommand{Pattern: normalizeQuery(found[1])}
	}
	if text[0] != '/' {
		return &SearchCommand{Pattern: normalizeQuery(text)}
	}
	return nil
}

func normalizeQuery(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func (cmd *SearchCommand) Handle(ctx context.Context, upd *telegram.Update) error {
	if cmd.Pattern == "" {
		return SendMessage(ctx, cmd.TgApi, telegram.SendMessageParams{
			ChatID: upd.Message.Chat.ID,
			Text:   "Usage: /search <query>, or just send the text to search for.",
		})
	}
	logger.Info("Starting search, pattern: %s", cmd.Pattern)
	cfg := cmd.RutrackerConfig
	rutrackerClient, err := rutracker.NewAuthenticatedRutrackerClient(cfg.Username, cfg.Password, rutracker.WithTimeout(30*time.Second))
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

	chatID := upd.Message.Chat.ID
	if len(found) == 0 {
		SendMessage(ctx, cmd.TgApi, telegram.SendMessageParams{
			ChatID: chatID,
			Text:   "Nothing found",
		})
		return nil
	}

	sort.Slice(found, func(i, j int) bool {
		return found[i].Seeders > found[j].Seeders
	})

	for _, f := range found[:min(10, len(found))] {
		// The results are the command's output: stop at the first one that
		// cannot be sent (after SendMessage's flood-control retry).
		if err := SendMessage(ctx, cmd.TgApi, telegram.SendMessageParams{
			ChatID: chatID,
			Text:   fmt.Sprintf("%v\r\n\r\nSize:%v%v\r\nSeeders: %v", f.Title, f.Size.Size, f.Size.Unit, f.Seeders),
			ReplyMarkup: &telegram.InlineKeyboardMarkup{
				InlineKeyboard: [][]telegram.InlineKeyboardButton{
					{
						{
							Text:         "Download",
							CallbackData: fmt.Sprintf("/dl %v", f.DownloadURL),
						},
					},
				},
			},
		}); err != nil {
			return err
		}
	}
	return nil
}
