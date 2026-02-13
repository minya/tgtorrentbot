package commands

import (
	"github.com/minya/telegram"
)

type CommandFactory interface {
	Accepts(upd *telegram.Update) (bool, Command)
}

type Command interface {
	Handle(upd *telegram.Update) error
}

func AnswerCallbackQuery(upd *telegram.Update, api *telegram.Api) {
	if upd.CallbackQuery != nil {
		api.AnswerCallbackQuery(&telegram.AnswerCallbackQueryParams{
			CallbackQueryID: upd.CallbackQuery.Id,
			Text:            "Processing...",
			ShowAlert:       false,
		})
	}
}

// func ParseCommand(cmdText string) (ok bool, cmd any) {
// 	reListCmd := regexp.MustCompile(`^/list\s*?$`)
// 	reRemCmd := regexp.MustCompile(`^/remove\s(\d+)\s*?$`)
// 	reDlCmd := regexp.MustCompile(`^/dl\s(.+?)$`)

// 	if matched := reListCmd.Match([]byte(cmdText)); matched {
// 		return true, ListCommand{}
// 	}

// 	if found := reRemCmd.FindStringSubmatch(cmdText); len(found) == 2 {
// 		torrentID, err := strconv.Atoi(found[1])
// 		if err != nil {
// 			return false, nil
// 		}

// 		return true, RemoveTorrentCommand{TorrentID: torrentID}
// 	}

// 	if found := reSearchCmd.FindStringSubmatch(cmdText); len(found) == 2 {
// 		pattern := strings.TrimSpace(found[1])
// 		return true, SearchCommand{Pattern: pattern}
// 	}

// 	if found := reDlCmd.FindStringSubmatch(cmdText); len(found) == 2 {
// 		url := strings.TrimSpace(found[1])
// 		return true, DownloadCommand{URL: url}
// 	}

// 	return false, nil
// }
