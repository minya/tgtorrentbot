package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/internal/autofix"
)

// The fixer's status is edited into the "Completed" notice, below its
// text; the report is a reply to it.
func TestTelegramNotifier(t *testing.T) {
	type call struct {
		method string
		body   map[string]any
	}
	var calls []call
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		calls = append(calls, call{r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:], body})
		w.Write([]byte(`{"ok":true,"result":{"message_id":2,"chat":{"id":42}}}`))
	}))
	defer srv.Close()
	n := telegramNotifier{telegram.NewAPI("tok", telegram.WithBaseURL(srv.URL))}
	item := &autofix.Item{ChatID: 42, ReplyTo: 7, Notice: "Completed: X [TV Shows]"}

	if err := n.Mark(context.Background(), item, autofix.StatusClean); err != nil {
		t.Fatal(err)
	}
	if err := n.Reply(context.Background(), item, "Auto-fix: X"); err != nil {
		t.Fatal(err)
	}
	if err := n.Mark(context.Background(), &autofix.Item{ChatID: 42}, autofix.StatusClean); err != nil {
		t.Fatal(err) // no notice: nothing to edit
	}
	if len(calls) != 2 || calls[0].method != "editMessageText" || calls[1].method != "sendMessage" {
		t.Fatalf("calls %+v", calls)
	}
	if got := calls[0].body["text"]; got != "Completed: X [TV Shows]\n"+autofix.StatusClean || calls[0].body["message_id"] != float64(7) {
		t.Fatalf("edit %+v", calls[0].body)
	}
	if rp, _ := calls[1].body["reply_parameters"].(map[string]any); rp["message_id"] != float64(7) {
		t.Fatalf("reply %+v", calls[1].body)
	}
}
