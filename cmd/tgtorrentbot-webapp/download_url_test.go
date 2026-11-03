package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/minya/rutracker"
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

// F2: a failed rutracker login says why.
func TestRutrackerLoginErrorMessages(t *testing.T) {
	for err, want := range map[error]string{
		rutracker.ErrCaptcha:     "captcha",
		rutracker.ErrLoginFailed: "username and password",
		errors.New("dial tcp"):   "failed to authenticate",
	} {
		rec := httptest.NewRecorder()
		writeRutrackerLoginError(rec, fmt.Errorf("login: %w", err))
		if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%v: %d %s, want 502 mentioning %q", err, rec.Code, rec.Body.String(), want)
		}
	}
}
