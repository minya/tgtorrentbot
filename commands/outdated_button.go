package commands

import (
	"context"
	"fmt"

	"github.com/minya/logger"
	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/environment"
)

// outdatedCategoryButtonCommand answers a category button whose category no
// longer exists (a keyboard sent before a category was renamed or removed).
// Accepting it, rather than rejecting the callback, means the button press
// is answered and the user learns why nothing happened.
type outdatedCategoryButtonCommand struct {
	Category string
	environment.Env
}

func (cmd *outdatedCategoryButtonCommand) Handle(ctx context.Context, upd *telegram.Update) error {
	AnswerCallbackQuery(ctx, upd, cmd.TgApi)
	logger.Warn("Category button with unknown category %q: %q", cmd.Category, upd.CallbackQuery.Data)
	return SendMessage(ctx, cmd.TgApi, telegram.SendMessageParams{
		ChatID: upd.CallbackQuery.Message.Chat.ID,
		Text:   fmt.Sprintf("This button is outdated (unknown category %q). Please start the download again.", cmd.Category),
	})
}
