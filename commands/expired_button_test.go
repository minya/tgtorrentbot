package commands

import (
	"context"
	"slices"
	"testing"

	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/internal"
)

// F77: an expired magnet button asks for the magnet link, an expired file
// button for the file.
func TestExpiredButtonsAskForTheRightThing(t *testing.T) {
	for data, want := range map[string]string{
		"/dlmagnet movies 00000000-0000-0000-0000-000000000000":  expiredMagnetText,
		"/dlfilecat movies 00000000-0000-0000-0000-000000000000": expiredFileText,
	} {
		env, log := failingEnv(t, `{}`)
		env.FileIDLookup = &internal.FileIDLookup{}
		upd := &telegram.Update{CallbackQuery: &telegram.CallbackQuery{
			ID: "q", Data: data, From: &telegram.User{ID: 42},
			Message: &telegram.Message{MessageID: 1, Chat: telegram.Chat{ID: 42}},
		}}
		var cmd Command
		for _, f := range []CommandFactory{&DownloadMagnetWithCategoryCommandFactory{Env: env}, &DownloadFileWithCategoryCommandFactory{Env: env}} {
			if ok, c := f.Accepts(upd); ok {
				cmd = c
			}
		}
		if cmd == nil {
			t.Fatalf("%q not accepted", data)
		}
		_ = cmd.Handle(context.Background(), upd)
		if _, texts := log.snapshot(); !slices.Contains(texts, want) {
			t.Errorf("%q: user got %q, want %q", data, texts, want)
		}
	}
}
