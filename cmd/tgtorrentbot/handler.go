package main

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/minya/logger"
	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/commands"
	"github.com/minya/tgtorrentbot/environment"
	"github.com/odwrtw/transmission"
)

type UpdatesHandler struct {
	notify func()
	environment.Env
	commandsList []commands.CommandFactory
}

func NewUpdatesHandler(env environment.Env, notifyFunc func()) *UpdatesHandler {
	return &UpdatesHandler{
		notify: notifyFunc,
		Env:    env,

		commandsList: []commands.CommandFactory{
			&commands.ListCommandFactory{Env: env},
			&commands.ListPageCommandFactory{Env: env},
			&commands.RemoveTorrentCommandFactory{Env: env},
			&commands.DownloadByMagnetCommandFactory{Env: env}, // Must come before SearchCommandFactory
			&commands.SearchCommandFactory{Env: env},
			&commands.DownloadWithCategoryCommandFactory{Env: env},     // Must come before DownloadCommandFactory
			&commands.DownloadFileWithCategoryCommandFactory{Env: env}, // Must come before DownloadByFileCommandFactory
			&commands.DownloadMagnetWithCategoryCommandFactory{Env: env},
			&commands.DownloadCommandFactory{Env: env},
			&commands.DownloadByFileCommandFactory{Env: env},
		},
	}
}

func extractUser(upd *telegram.Update) *telegram.User {
	if upd.Message != nil && upd.Message.From != nil {
		return upd.Message.From
	}
	if upd.CallbackQuery != nil {
		return upd.CallbackQuery.From
	}
	return nil
}

func (handler *UpdatesHandler) HandleUpdate(ctx context.Context, upd *telegram.Update) error {
	logger.Debug("Received update: %+v", upd)
	user := extractUser(upd)
	if user == nil {
		logger.Warn("Ignoring update with no user info")
		return nil
	}
	if !slices.Contains(handler.AllowedUsers, user.ID) {
		logger.Warn("Unauthorized access attempt: id=%d username=%s name=%s %s",
			user.ID, user.Username, user.FirstName, user.LastName)
		return nil
	}
	logger.Debug("Authorized user: id=%d username=%s", user.ID, user.Username)

	// Every callback command replies into the chat of the message carrying
	// the button. Telegram omits that message for buttons on inline-mode
	// messages (only inline_message_id is set), so reject those here instead
	// of letting each command dereference a nil Message.
	if upd.CallbackQuery != nil && upd.CallbackQuery.Message == nil {
		logger.Warn("Ignoring callback query %q without a message", upd.CallbackQuery.Data)
		handler.TgApi.AnswerCallbackQuery(ctx, telegram.AnswerCallbackQueryParams{
			CallbackQueryID: upd.CallbackQuery.ID,
			Text:            "This button only works in the bot chat",
		})
		return nil
	}

	for _, factory := range handler.commandsList {
		accepts, cmd := factory.Accepts(upd)
		if accepts {
			logger.Debug("Command accepted: %T", cmd)
			handleErr := cmd.Handle(ctx, upd)
			if handleErr == nil {
				handler.notify()
			} else if !commands.IsReplied(handleErr) {
				// Most failure paths return without messaging the chat;
				// without this the user sees "Processing..." and silence.
				if chatID, ok := replyChatID(upd); ok {
					handler.TgApi.SendMessage(ctx, telegram.SendMessageParams{
						ChatID: chatID,
						Text:   failureText(handleErr),
					})
				}
			}
			return handleErr
		}
	}

	chatID, ok := replyChatID(upd)
	if !ok {
		return nil
	}

	handler.TgApi.SendMessage(ctx, telegram.SendMessageParams{
		ChatID: chatID,
		Text:   "Expected a command, search query, or torrent file",
	})
	return nil
}

// replyChatID returns the chat a reply to upd goes to.
func replyChatID(upd *telegram.Update) (int64, bool) {
	switch {
	case upd.Message != nil && upd.Message.MessageID != 0:
		return upd.Message.Chat.ID, true
	case upd.CallbackQuery != nil && upd.CallbackQuery.Message != nil:
		return upd.CallbackQuery.Message.Chat.ID, true
	}
	return 0, false
}

// failureText is the chat reply for a command error the command did not
// report itself.
func failureText(err error) string {
	if errors.Is(err, transmission.ErrDuplicateTorrent) {
		return "This torrent is already in Transmission."
	}
	return fmt.Sprintf("Something went wrong: %v", err)
}
