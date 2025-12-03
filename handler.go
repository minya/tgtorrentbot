package main

import (
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
			&commands.RemoveTorrentCommandFactory{Env: env},
			&commands.SearchCommandFactory{Env: env},
			&commands.DownloadCommandFactory{Env: env},
			&commands.DownloadByFileCommandFactory{Env: env},
		},
	}
}

func (handler *UpdatesHandler) HandleUpdate(upd *telegram.Update) error {
	var replyChatID int64

	switch {
	case upd.Message != nil && upd.Message.MessageId != 0:
		replyChatID = upd.Message.Chat.Id
	case upd.CallbackQuery != nil && upd.CallbackQuery.Message != nil:
		replyChatID = upd.CallbackQuery.Message.Chat.Id
	default:
		return nil
	}

	for _, factory := range handler.commandsList {
		accepts, cmd := factory.Accepts(upd)
		if accepts {
			handleErr := cmd.Handle(replyChatID)
			if handleErr == nil {
				handler.notify()
			}
			return handleErr
		}
	}

	handler.TgApi.SendMessage(telegram.ReplyMessage{
		ChatId: replyChatID,
		Text:   "Ожидается команда, запрос или torrent-файл", // TODO: translate
	})
	return nil
}
