package commands

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/environment"
)

// scriptedTelegram answers the n-th call with replies[n] (the last one repeats).
func scriptedTelegram(t *testing.T, replies ...string) (*telegram.API, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(calls.Add(1)) - 1
		w.Write([]byte(replies[min(n, len(replies)-1)]))
	}))
	t.Cleanup(srv.Close)
	return telegram.NewAPI("tok", telegram.WithBaseURL(srv.URL)), &calls
}

const (
	tgOK       = `{"ok":true,"result":{"message_id":1,"chat":{"id":1}}}`
	tgFlood    = `{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 1","parameters":{"retry_after":1}}`
	tgBadReq   = `{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`
	tgNotModif = `{"ok":false,"error_code":400,"description":"Bad Request: message is not modified"}`
)

// F45: a rate-limited send is retried once after retry_after instead of
// silently dropped.
func TestSendMessageRetriesAfterFloodControl(t *testing.T) {
	api, calls := scriptedTelegram(t, tgFlood, tgOK)
	start := time.Now()
	if err := SendMessage(context.Background(), api, telegram.SendMessageParams{ChatID: 1, Text: "x"}); err != nil {
		t.Fatalf("SendMessage = %v, want nil after the retry", err)
	}
	if calls.Load() != 2 || time.Since(start) < time.Second {
		t.Fatalf("calls = %d after %s; want 2 calls, the second after retry_after", calls.Load(), time.Since(start))
	}
}

// F45: a failure is returned, not swallowed.
func TestSendMessageReportsFailure(t *testing.T) {
	api, calls := scriptedTelegram(t, tgBadReq)
	if err := SendMessage(context.Background(), api, telegram.SendMessageParams{ChatID: 1, Text: "x"}); err == nil {
		t.Fatal("SendMessage = nil for a 400")
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1 (no retry for a 400)", calls.Load())
	}
}

func TestEditMessageTextNotModifiedIsFine(t *testing.T) {
	api, _ := scriptedTelegram(t, tgNotModif)
	if err := EditMessageText(context.Background(), api, telegram.EditMessageTextParams{ChatID: 1, MessageID: 1, Text: "x"}); err != nil {
		t.Fatalf("EditMessageText = %v for 'message is not modified'", err)
	}
	api, _ = scriptedTelegram(t, tgBadReq)
	if err := EditMessageText(context.Background(), api, telegram.EditMessageTextParams{ChatID: 1, MessageID: 1, Text: "x"}); err == nil {
		t.Fatal("EditMessageText = nil for a real failure")
	}
}

// A failed /list send is now the command's error (the handler then tells
// the user), instead of reported as success.
func TestSendTorrentsListReturnsSendError(t *testing.T) {
	api, _ := scriptedTelegram(t, tgBadReq)
	tc, _ := labelRecorder(t)
	err := sendTorrentsList(context.Background(), environment.Env{TransmissionClient: tc, TgApi: api}, 1, 1)
	if err == nil {
		t.Fatal("sendTorrentsList = nil although the message was not sent")
	}
}
