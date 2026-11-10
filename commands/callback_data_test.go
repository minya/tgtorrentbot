package commands

import (
	"testing"

	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/internal"
)

// Telegram rejects an inline button whose callback_data exceeds 64 bytes,
// and the keyboard then silently does nothing. The category name is the
// variable part of every category keyboard, so a new, longer category must
// fail here rather than in production.
const maxCallbackData = 64

func TestCategoryCallbacksFitTelegramLimit(t *testing.T) {
	var lookup internal.FileIDLookup
	key := lookup.Add("x") // a real UUID key, as the file and magnet flows use
	// A rutracker download URL as search results carry it; topic ids have
	// 7 digits today, allow for 10.
	dl := &DownloadCommand{URL: "dl.php?t=9999999999"}

	for name, kb := range map[string]telegram.InlineKeyboardMarkup{
		"/dlfilecat": buildCategoryKeyboard(key),
		"/dlmagnet":  buildMagnetCategoryKeyboard(key),
		"/dlcat":     dl.buildCategoryKeyboard(),
	} {
		if len(kb.InlineKeyboard) != len(AllCategories()) {
			t.Fatalf("%s: %d buttons for %d categories", name, len(kb.InlineKeyboard), len(AllCategories()))
		}
		for _, row := range kb.InlineKeyboard {
			for _, b := range row {
				if len(b.CallbackData) > maxCallbackData {
					t.Errorf("%s: callback %q is %d bytes, Telegram allows %d; shorten the category name",
						name, b.CallbackData, len(b.CallbackData), maxCallbackData)
				}
			}
		}
	}
}
