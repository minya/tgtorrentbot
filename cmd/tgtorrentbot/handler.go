package main

import (
	"slices"

	"github.com/minya/logger"
	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/commands"
	"github.com/minya/tgtorrentbot/environment"
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
			&commands.SearchCommandFactory{Env: env},
			&commands.DownloadWithCategoryCommandFactory{Env: env},       // Must come before DownloadCommandFactory
			&commands.DownloadFileWithCategoryCommandFactory{Env: env},   // Must come before DownloadByFileCommandFactory
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

func (handler *UpdatesHandler) HandleUpdate(upd *telegram.Update) error {
	user := extractUser(upd)
	if user == nil {
		logger.Warn("Ignoring update with no user info")
		return nil
	}
	if !slices.Contains(handler.AllowedUsers, user.Id) {
		logger.Warn("Unauthorized access attempt: id=%d username=%s name=%s %s",
			user.Id, user.UserName, user.FirstName, user.LastName)
		return nil
	}
	logger.Debug("Authorized user: id=%d username=%s", user.Id, user.UserName)

	for _, factory := range handler.commandsList {
		accepts, cmd := factory.Accepts(upd)
		if accepts {
			handleErr := cmd.Handle(upd)
			if handleErr == nil {
				handler.notify()
			}
			return handleErr
		}
	}

	var replyChatID int64
	switch {
	case upd.Message != nil && upd.Message.MessageId != 0:
		replyChatID = upd.Message.Chat.Id
	case upd.CallbackQuery != nil && upd.CallbackQuery.Message != nil:
		replyChatID = upd.CallbackQuery.Message.Chat.Id
	default:
		return nil
	}

	handler.TgApi.SendMessage(telegram.ReplyMessage{
		ChatId: replyChatID,
		Text:   "Expected a command, search query, or torrent file",
	})
	return nil
}
