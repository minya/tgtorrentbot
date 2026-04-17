package commands

import (
	"regexp"
	"strings"

	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/environment"
)

type DownloadMagnetWithCategoryCommand struct {
	MagnetURI string
	Category  Category
	environment.Env
}

type DownloadMagnetWithCategoryCommandFactory struct {
	environment.Env
}

var reDownloadMagnetWithCategoryCmd = regexp.MustCompile(`^/dlmagnet\s+(\S+)\s+(.+?)$`)

func (factory *DownloadMagnetWithCategoryCommandFactory) Accepts(upd *telegram.Update) (bool, Command) {
	if upd == nil || upd.CallbackQuery == nil {
		return false, nil
	}
	found := reDownloadMagnetWithCategoryCmd.FindStringSubmatch(upd.CallbackQuery.Data)
	if len(found) != 3 {
		return false, nil
	}
	categoryStr := strings.TrimSpace(found[1])
	memID := strings.TrimSpace(found[2])

	category, ok := ParseCategory(categoryStr)
	if !ok {
		return false, nil
	}

	magnetURI, ok := factory.FileIDLookup.Get(memID)
	if !ok {
		return true, &expiredFileButtonCommand{Env: factory.Env}
	}

	return true, &DownloadMagnetWithCategoryCommand{
		MagnetURI: magnetURI,
		Category:  category,
		Env:       factory.Env,
	}
}

func (cmd *DownloadMagnetWithCategoryCommand) Handle(upd *telegram.Update) error {
	AnswerCallbackQuery(upd, cmd.TgApi)

	downloadCmd := &DownloadCommand{Env: cmd.Env}
	chatID := upd.CallbackQuery.Message.Chat.Id
	return downloadCmd.addMagnetAndReply(cmd.MagnetURI, chatID, cmd.Category)
}
