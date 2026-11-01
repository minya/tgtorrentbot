package commands

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/environment"
	"github.com/odwrtw/transmission"
)

type rpcLog struct {
	mu      sync.Mutex
	methods []string
	texts   []string
}

func (l *rpcLog) add(dst *[]string, v string) { l.mu.Lock(); *dst = append(*dst, v); l.mu.Unlock() }
func (l *rpcLog) snapshot() ([]string, []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.methods), slices.Clone(l.texts)
}

// failingEnv returns an Env whose fake Transmission answers torrent-add with
// addBody and fails torrent-set, and whose fake Telegram records message texts.
func failingEnv(t *testing.T, addBody string) (environment.Env, *rpcLog) {
	t.Helper()
	log := &rpcLog{}
	tr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var req struct{ Method string }
		_ = json.Unmarshal(b, &req)
		log.add(&log.methods, req.Method)
		switch req.Method {
		case "torrent-add":
			w.Write([]byte(addBody))
		case "torrent-set":
			w.Write([]byte(`{"result":"invalid argument","arguments":{}}`))
		default:
			w.Write([]byte(`{"result":"success","arguments":{}}`))
		}
	}))
	t.Cleanup(tr.Close)
	tg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			Text string `json:"text"`
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &p)
		if p.Text != "" {
			log.add(&log.texts, p.Text)
		}
		w.Write([]byte(`{"ok":true,"result":{"message_id":1,"chat":{"id":1}}}`))
	}))
	t.Cleanup(tg.Close)
	tc, err := transmission.New(transmission.Config{Address: tr.URL})
	if err != nil {
		t.Fatal(err)
	}
	return environment.Env{TransmissionClient: tc, TgApi: telegram.NewAPI("tok", telegram.WithBaseURL(tg.URL)), DownloadPath: t.TempDir()}, log
}

func magnetCallback() *telegram.Update {
	return &telegram.Update{CallbackQuery: &telegram.CallbackQuery{
		ID: "q", From: &telegram.User{ID: 42}, Message: &telegram.Message{Chat: telegram.Chat{ID: 42}},
	}}
}

// F37: when labels cannot be set, the torrent is removed again and the user
// is told.
func TestLabelFailureRemovesTorrentAndReplies(t *testing.T) {
	env, log := failingEnv(t, `{"result":"success","arguments":{"torrent-added":{"id":3,"name":"X","hashString":"h"}}}`)
	cmd := &DownloadMagnetWithCategoryCommand{Env: env, MagnetURI: "magnet:?xt=urn:btih:abc", Category: CategoryMovies}

	if err := cmd.Handle(context.Background(), magnetCallback()); err == nil {
		t.Fatal("Handle returned nil for a failed label set")
	}
	methods, texts := log.snapshot()
	if !slices.Contains(methods, "torrent-remove") {
		t.Errorf("RPC sequence %v has no torrent-remove", methods)
	}
	if !slices.ContainsFunc(texts, func(s string) bool { return strings.Contains(s, "not added") }) {
		t.Errorf("user got %q, want a message saying it was not added", texts)
	}
}

// F37: a duplicate add is reported to the user instead of failing silently.
func TestDuplicateTorrentReplies(t *testing.T) {
	env, log := failingEnv(t, `{"result":"success","arguments":{"torrent-duplicate":{"id":3,"name":"X","hashString":"h"}}}`)
	cmd := &DownloadMagnetWithCategoryCommand{Env: env, MagnetURI: "magnet:?xt=urn:btih:abc", Category: CategoryMovies}

	_ = cmd.Handle(context.Background(), magnetCallback())
	_, texts := log.snapshot()
	if !slices.ContainsFunc(texts, func(s string) bool { return strings.Contains(s, "already in Transmission") }) {
		t.Errorf("user got %q, want an 'already in Transmission' message", texts)
	}
}
