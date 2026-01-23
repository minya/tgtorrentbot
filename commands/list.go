package commands

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/minya/logger"
	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/environment"
	"github.com/odwrtw/transmission"
)

const pageSize = 5

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

func (cmd *ListCommand) Handle(upd *telegram.Update) error {
	chatID := upd.Message.Chat.Id
	return sendTorrentsList(cmd.Env, chatID, 0)
}

// ListPageCommand handles pagination callbacks for /list
type ListPageCommand struct {
	Page int
	environment.Env
}

type ListPageCommandFactory struct {
	environment.Env
}

var reListPageCmd = regexp.MustCompile(`^/list_page\s+(\d+)$`)

func (f *ListPageCommandFactory) Accepts(upd *telegram.Update) (bool, Command) {
	if upd == nil || upd.CallbackQuery == nil {
		return false, nil
	}
	if found := reListPageCmd.FindStringSubmatch(upd.CallbackQuery.Data); len(found) == 2 {
		page, _ := strconv.Atoi(found[1])
		return true, &ListPageCommand{Page: page, Env: f.Env}
	}
	return false, nil
}

func (cmd *ListPageCommand) Handle(upd *telegram.Update) error {
	AnswerCallbackQuery(upd, cmd.TgApi)
	chatID := upd.CallbackQuery.Message.Chat.Id
	messageID := upd.CallbackQuery.Message.MessageId
	return editTorrentsList(cmd.Env, chatID, messageID, cmd.Page)
}

// sendTorrentsList sends a new message with the torrent list (for initial /list command)
func sendTorrentsList(env environment.Env, chatID int64, page int) error {
	text, keyboard, err := prepareTorrentsList(env, page)
	if err != nil {
		env.TgApi.SendMessage(telegram.ReplyMessage{
			Text:   "Ошибка",
			ChatId: chatID,
		})
		return err
	}

	msg := telegram.ReplyMessage{
		Text:   text,
		ChatId: chatID,
	}
	if keyboard != nil {
		msg.ReplyMarkup = keyboard
	}

	env.TgApi.SendMessage(msg)
	return nil
}

// editTorrentsList edits an existing message with the torrent list (for pagination)
func editTorrentsList(env environment.Env, chatID int64, messageID int64, page int) error {
	text, keyboard, err := prepareTorrentsList(env, page)
	if err != nil {
		return err
	}

	params := &telegram.EditMessageTextParams{
		ChatID:    chatID,
		MessageID: messageID,
		Text:      text,
	}
	if keyboard != nil {
		params.ReplyMarkup = keyboard
	}

	env.TgApi.EditMessageText(params)
	return nil
}

// prepareTorrentsList fetches torrents and prepares the text and keyboard for display
func prepareTorrentsList(env environment.Env, page int) (string, *telegram.InlineKeyboardMarkup, error) {
	torrents, err := env.TransmissionClient.GetTorrents()
	if err != nil {
		logger.Error(err, "Error getting torrents")
		return "", nil, err
	}
	logger.Debug(fmt.Sprintf("Torrents received, count: %d", len(torrents)))

	if len(torrents) == 0 {
		return "Нет активных торрентов", nil, nil // TODO: translate
	}

	// Sort by ID descending (most recent first)
	sort.Slice(torrents, func(i, j int) bool {
		return torrents[i].ID > torrents[j].ID
	})

	totalPages := (len(torrents) + pageSize - 1) / pageSize
	if page < 0 {
		page = 0
	}
	if page >= totalPages {
		page = totalPages - 1
	}

	start := page * pageSize
	end := start + pageSize
	if end > len(torrents) {
		end = len(torrents)
	}

	pageTorrents := torrents[start:end]

	text := formatTorrentsList(pageTorrents, page, totalPages, len(torrents))
	keyboard := buildPaginationKeyboard(page, totalPages)

	return text, keyboard, nil
}

func formatTorrentsList(torrents []*transmission.Torrent, page, totalPages, total int) string {
	var sb strings.Builder

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
		fmt.Fprintf(&sb, "%v [%s] %v %.0f%%\n\n", torrent.ID, categoryLabel, torrent.Name, torrent.PercentDone*100)
	}

	fmt.Fprintf(&sb, "Страница %d/%d (всего: %d)", page+1, totalPages, total)
	return sb.String()
}

func buildPaginationKeyboard(page, totalPages int) *telegram.InlineKeyboardMarkup {
	if totalPages <= 1 {
		return nil
	}

	var buttons []telegram.InlineKeyboardButton

	if page > 0 {
		buttons = append(buttons, telegram.InlineKeyboardButton{
			Text:         "← Назад",
			CallbackData: fmt.Sprintf("/list_page %d", page-1),
		})
	}

	if page < totalPages-1 {
		buttons = append(buttons, telegram.InlineKeyboardButton{
			Text:         "Вперёд →",
			CallbackData: fmt.Sprintf("/list_page %d", page+1),
		})
	}

	if len(buttons) == 0 {
		return nil
	}

	return &telegram.InlineKeyboardMarkup{
		InlineKeyboard: [][]telegram.InlineKeyboardButton{buttons},
	}
}
