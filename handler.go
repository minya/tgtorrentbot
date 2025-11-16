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
	var replyChatID int
	if upd.Message.MessageId != 0 {
		replyChatID = upd.Message.Chat.Id
	} else {
		replyChatID = upd.CallbackQuery.Message.Chat.Id
	}

	handler.notify()

	for _, factory := range handler.commandsList {
		accepts, cmd := factory.Accepts(upd)
		if accepts {
			return cmd.Handle(replyChatID)
		}
	}

	handler.TgApi.SendMessage(telegram.ReplyMessage{
		ChatId: replyChatID,
		Text:   "Ожидается команда, запрос или torrent-файл", // TODO: translate
	})
	return nil
}
