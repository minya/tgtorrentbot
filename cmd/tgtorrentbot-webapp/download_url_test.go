package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// F84: only the relative dl.php?t=N form search results carry is accepted;
// absolute and crafted rutracker URLs are refused before anything is fetched.
func TestDownloadURLValidation(t *testing.T) {
	app := &App{}
	for _, u := range []string{
		"https://rutracker.org/forum/dl.php?t=123",
		"//rutracker.org/../login.php?logout=1",
		"http://www.rutracker.org/forum/login.php",
		"dl.php?t=1&x=2",
		"../dl.php?t=1",
		"viewtopic.php?t=1",
	} {
		rec := httptest.NewRecorder()
		body := `{"downloadUrl":"` + u + `","category":"movies"}`
		app.handleDownloadTorrent(42, rec, httptest.NewRequest(http.MethodPost, "/api/torrents/download", strings.NewReader(body)))
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid downloadUrl") {
			t.Errorf("%q: status %d %s, want 400 invalid downloadUrl", u, rec.Code, rec.Body.String())
		}
	}
	for _, u := range []string{"dl.php?t=123", "dl.php?t=1"} {
		if !relativeDownloadRe.MatchString(u) {
			t.Errorf("%q rejected", u)
		}
	}
}
