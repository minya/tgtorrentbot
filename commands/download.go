package commands

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/minya/logger"
	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/environment"
	"github.com/minya/tgtorrentbot/internal/torrentlabels"
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
	if upd == nil || upd.CallbackQuery == nil {
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

func (cmd *DownloadCommand) Handle(ctx context.Context, upd *telegram.Update) error {
	AnswerCallbackQuery(ctx, upd, cmd.TgApi)

	keyboard := cmd.buildCategoryKeyboard()
	cmd.TgApi.SendMessage(ctx, telegram.SendMessageParams{
		ChatID:      upd.CallbackQuery.Message.Chat.ID,
		Text:        "Select category:",
		ReplyMarkup: keyboard,
	})
	return nil
}

func (cmd *DownloadCommand) buildCategoryKeyboard() telegram.InlineKeyboardMarkup {
	var buttons [][]telegram.InlineKeyboardButton

	for _, cat := range AllCategories() {
		button := telegram.InlineKeyboardButton{
			Text:         cat.DisplayName(),
			CallbackData: fmt.Sprintf("/dlcat %s %s", cat.String(), cmd.URL),
		}
		buttons = append(buttons, []telegram.InlineKeyboardButton{button})
	}

	return telegram.InlineKeyboardMarkup{
		InlineKeyboard: buttons,
	}
}

func (cmd *DownloadCommand) addTorrentAndReply(ctx context.Context, content []byte, chatID, userID int64, category Category) error {
	torrentBase64 := base64.StdEncoding.EncodeToString(content)
	downloadDir := fmt.Sprintf("%s/%s", cmd.DownloadPath, category.String())

	logger.Debug("Adding torrent with category %s to directory %s", category.String(), downloadDir)

	torrent, err := cmd.TransmissionClient.AddTorrent(transmission.AddTorrentArg{
		Metainfo:    torrentBase64,
		DownloadDir: downloadDir,
	})
	if err != nil {
		return cmd.replyAddFailed(ctx, chatID, err)
	}

	return cmd.finalizeTorrent(ctx, torrent, chatID, userID, category)
}

func (cmd *DownloadCommand) addMagnetAndReply(ctx context.Context, magnetURI string, chatID, userID int64, category Category) error {
	downloadDir := fmt.Sprintf("%s/%s", cmd.DownloadPath, category.String())

	logger.Debug("Adding magnet with category %s to directory %s", category.String(), downloadDir)

	torrent, err := cmd.TransmissionClient.AddTorrent(transmission.AddTorrentArg{
		Filename:    magnetURI,
		DownloadDir: downloadDir,
	})
	if err != nil {
		return cmd.replyAddFailed(ctx, chatID, err)
	}

	return cmd.finalizeTorrent(ctx, torrent, chatID, userID, category)
}

// replyAddFailed tells the user why Transmission did not add the torrent and
// returns err for the handler's log.
func (cmd *DownloadCommand) replyAddFailed(ctx context.Context, chatID int64, err error) error {
	text := fmt.Sprintf("Transmission did not add the torrent: %v", err)
	if errors.Is(err, transmission.ErrDuplicateTorrent) {
		text = "This torrent is already in Transmission."
	}
	logger.Error(err, "Error from transmission RPC")
	cmd.TgApi.SendMessage(ctx, telegram.SendMessageParams{ChatID: chatID, Text: text})
	return err
}

func (cmd *DownloadCommand) finalizeTorrent(ctx context.Context, torrent *transmission.Torrent, chatID, userID int64, category Category) error {
	labels := torrentlabels.New(chatID, userID, category.String())
	logger.Debug("Torrent added with ID %v, setting labels to %v", torrent.ID, labels)

	if err := torrent.Set(transmission.SetTorrentArg{Labels: labels}); err != nil {
		// Without labels the torrent would get no completion notification
		// and be invisible in the webapp, and a retry would only report a
		// duplicate. Take it back out (data kept), as the webapp does.
		logger.Error(err, "Error setting torrent labels, removing torrent %d", torrent.ID)
		if rmErr := cmd.TransmissionClient.RemoveTorrents([]*transmission.Torrent{torrent}, false); rmErr != nil {
			logger.Error(rmErr, "Failed to remove unlabelled torrent %d", torrent.ID)
			cmd.TgApi.SendMessage(ctx, telegram.SendMessageParams{
				ChatID: chatID,
				Text:   fmt.Sprintf("Added %v, but could not set its category (%v) or remove it. Remove it in Transmission and try again.", torrent.ID, err),
			})
			return err
		}
		cmd.TgApi.SendMessage(ctx, telegram.SendMessageParams{
			ChatID: chatID,
			Text:   fmt.Sprintf("Could not set the torrent's category (%v); it was not added. Please try again.", err),
		})
		return err
	}

	logger.Debug("Torrent %v labels set successfully", torrent.ID)

	cmd.TgApi.SendMessage(ctx, telegram.SendMessageParams{
		ChatID: chatID,
		Text:   fmt.Sprintf("Added: %v [%s]", torrent.ID, category.DisplayName()),
	})
	return nil
}

// callbackOrigin returns the chat a category button was pressed in, which
// gets the replies and the completion notification, and the user who pressed
// it, who owns the torrent. They differ in a group chat.
func callbackOrigin(upd *telegram.Update) (chatID, userID int64) {
	chatID = upd.CallbackQuery.Message.Chat.ID
	userID = chatID
	if upd.CallbackQuery.From != nil {
		userID = upd.CallbackQuery.From.ID
	}
	return chatID, userID
}
