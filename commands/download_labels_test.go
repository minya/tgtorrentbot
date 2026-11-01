package commands

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/environment"
	"github.com/odwrtw/transmission"
)

// labelRecorder is a fake Transmission that accepts torrent-add and records
// the labels of every torrent-set.
func labelRecorder(t *testing.T) (*transmission.Client, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var labels []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var req struct {
			Method    string
			Arguments struct{ Labels []string }
		}
		_ = json.Unmarshal(b, &req)
		switch req.Method {
		case "torrent-add":
			w.Write([]byte(`{"result":"success","arguments":{"torrent-added":{"id":3,"name":"X","hashString":"h"}}}`))
		case "torrent-set":
			mu.Lock()
			labels = req.Arguments.Labels
			mu.Unlock()
			w.Write([]byte(`{"result":"success","arguments":{}}`))
		default:
			w.Write([]byte(`{"result":"success","arguments":{}}`))
		}
	}))
	t.Cleanup(srv.Close)
	c, err := transmission.New(transmission.Config{Address: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	return c, func() []string { mu.Lock(); defer mu.Unlock(); return slices.Clone(labels) }
}

func fakeTgAPI(t *testing.T) *telegram.API {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true,"result":{"message_id":1,"chat":{"id":1}}}`))
	}))
	t.Cleanup(srv.Close)
	return telegram.NewAPI("tok", telegram.WithBaseURL(srv.URL))
}

// F83: a download started from a group chat records the group as the chat
// (for the notification) and the pressing user as the owner, so the webapp,
// which filters by user, shows it.
func TestGroupDownloadLabelsOwner(t *testing.T) {
	tc, got := labelRecorder(t)
	cmd := &DownloadMagnetWithCategoryCommand{
		Env:       environment.Env{TransmissionClient: tc, TgApi: fakeTgAPI(t), DownloadPath: t.TempDir()},
		MagnetURI: "magnet:?xt=urn:btih:abc",
		Category:  CategoryMovies,
	}
	upd := &telegram.Update{CallbackQuery: &telegram.CallbackQuery{
		ID:      "q",
		From:    &telegram.User{ID: 42},
		Message: &telegram.Message{Chat: telegram.Chat{ID: -100123}},
	}}

	if err := cmd.Handle(context.Background(), upd); err != nil {
		t.Fatal(err)
	}
	if want := []string{"-100123", "movies", "42"}; !slices.Equal(got(), want) {
		t.Fatalf("labels = %v, want %v", got(), want)
	}
}
