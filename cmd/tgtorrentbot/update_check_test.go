package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/minya/telegram"
	"github.com/odwrtw/transmission"
)

// fakeTorrent is the state the fake Transmission reports for one torrent.
type fakeTorrent struct {
	mu          sync.Mutex
	percentDone float64
	doneDate    int64
}

func (f *fakeTorrent) set(percentDone float64, doneDate int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.percentDone, f.doneDate = percentDone, doneDate
}

func fakeTransmission(t *testing.T, ft *fakeTorrent) *transmission.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		ft.mu.Lock()
		torrent := map[string]any{
			"id": 1, "hashString": "h1", "name": "X",
			"percentDone": ft.percentDone, "doneDate": ft.doneDate,
			"labels": []string{"42", "movies"},
		}
		ft.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"result": "success", "arguments": map[string]any{
			"torrents": []map[string]any{torrent}}})
	}))
	t.Cleanup(srv.Close)
	c, err := transmission.New(transmission.Config{Address: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func fakeTelegram(t *testing.T, sent *atomic.Int32) *telegram.API {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent.Add(1)
		w.Write([]byte(`{"ok":true,"result":{"message_id":1,"chat":{"id":42}}}`))
	}))
	t.Cleanup(srv.Close)
	return telegram.NewAPI("tok", telegram.WithBaseURL(srv.URL))
}

func TestCompletionNotifiedOnTransition(t *testing.T) {
	ft := &fakeTorrent{}
	ft.set(0.5, 0)
	var sent atomic.Int32
	tc, api := fakeTransmission(t, ft), fakeTelegram(t, &sent)
	start := time.Now().Unix()

	st, err := updateCheckRoutine(tc, api, nil, start)
	if err != nil {
		t.Fatal(err)
	}
	ft.set(1, start+30)
	st, _ = updateCheckRoutine(tc, api, st, start+60)
	_, _ = updateCheckRoutine(tc, api, st, start+120)

	if got := sent.Load(); got != 1 {
		t.Fatalf("sent %d notifications, want 1", got)
	}
}

// A torrent the checker first sees already complete (bot restart, webapp
// add, or added and completed between two polls) is notified once when it
// completed after the previous poll.
func TestCompletionNotifiedWhenFirstSeenComplete(t *testing.T) {
	ft := &fakeTorrent{}
	since := time.Now().Unix()
	ft.set(1, since+5)
	var sent atomic.Int32
	tc, api := fakeTransmission(t, ft), fakeTelegram(t, &sent)

	st, err := updateCheckRoutine(tc, api, nil, since)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = updateCheckRoutine(tc, api, st, since+60)

	if got := sent.Load(); got != 1 {
		t.Fatalf("sent %d notifications, want 1", got)
	}
}

// Torrents that were complete before the checker started are not reported
// again after a restart.
func TestNoNotificationForTorrentCompletedBeforeStart(t *testing.T) {
	ft := &fakeTorrent{}
	since := time.Now().Unix()
	ft.set(1, since-3600)
	var sent atomic.Int32
	tc, api := fakeTransmission(t, ft), fakeTelegram(t, &sent)

	if _, err := updateCheckRoutine(tc, api, nil, since); err != nil {
		t.Fatal(err)
	}
	if got := sent.Load(); got != 0 {
		t.Fatalf("sent %d notifications, want 0", got)
	}
}

func TestNewlyCompletedIgnoresMissingDoneDate(t *testing.T) {
	cur := transmission.TorrentMap{"h": {Name: "X", PercentDone: 1}}
	if got := newlyCompleted(nil, cur, 0); len(got) != 0 {
		t.Fatalf("got %d, want 0 for a complete torrent without doneDate", len(got))
	}
}

// The checker polls by itself from start-up: no bot command is needed for it
// to observe a torrent added elsewhere.
func TestCheckerPollsWithoutNotify(t *testing.T) {
	ft := &fakeTorrent{}
	ft.set(1, time.Now().Unix()+1)
	var sent atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	_, done := CreateCompletedCheckRoutine(ctx, fakeTransmission(t, ft), fakeTelegram(t, &sent))
	t.Cleanup(func() { cancel(); <-done })

	deadline := time.Now().Add(5 * time.Second)
	for sent.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if sent.Load() != 1 {
		t.Fatalf("sent %d notifications without notify(), want 1", sent.Load())
	}
}

// flakyTelegram fails the first fails sendMessage calls with status, then
// succeeds; it counts every call.
func flakyTelegram(t *testing.T, status, fails int, calls *atomic.Int32) *telegram.API {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if int(calls.Add(1)) <= fails {
			w.Write([]byte(fmt.Sprintf(`{"ok":false,"error_code":%d,"description":"fail"}`, status)))
			return
		}
		w.Write([]byte(`{"ok":true,"result":{"message_id":1,"chat":{"id":42}}}`))
	}))
	t.Cleanup(srv.Close)
	return telegram.NewAPI("tok", telegram.WithBaseURL(srv.URL))
}

// F69: a notification that fails transiently is retried on the next check
// instead of being lost.
func TestCompletionNoticeRetriedAfterTransientFailure(t *testing.T) {
	ft := &fakeTorrent{}
	ft.set(0.5, 0)
	var calls atomic.Int32
	tc, api := fakeTransmission(t, ft), flakyTelegram(t, 502, 1, &calls)
	start := time.Now().Unix()

	st, _ := updateCheckRoutine(tc, api, nil, start)
	ft.set(1, start+30)
	st, _ = updateCheckRoutine(tc, api, st, start+60)  // send fails (502)
	st, _ = updateCheckRoutine(tc, api, st, start+120) // retried, delivered
	_, _ = updateCheckRoutine(tc, api, st, start+180)  // nothing more

	if got := calls.Load(); got != 2 {
		t.Fatalf("sendMessage calls = %d, want 2 (one failure, one retry)", got)
	}
}

// A permanent failure (bot blocked: 403) is not retried every minute.
func TestCompletionNoticeNotRetriedOnPermanentFailure(t *testing.T) {
	ft := &fakeTorrent{}
	ft.set(0.5, 0)
	var calls atomic.Int32
	tc, api := fakeTransmission(t, ft), flakyTelegram(t, 403, 100, &calls)
	start := time.Now().Unix()

	st, _ := updateCheckRoutine(tc, api, nil, start)
	ft.set(1, start+30)
	st, _ = updateCheckRoutine(tc, api, st, start+60)
	_, _ = updateCheckRoutine(tc, api, st, start+120)

	if got := calls.Load(); got != 1 {
		t.Fatalf("sendMessage calls = %d, want 1", got)
	}
}

// F124: the checker stops when its context is done, so shutdown can wait
// for it and tests do not leak it.
func TestCheckerStopsOnCancel(t *testing.T) {
	ft := &fakeTorrent{}
	ft.set(0.5, 0)
	var sent atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	_, done := CreateCompletedCheckRoutine(ctx, fakeTransmission(t, ft), fakeTelegram(t, &sent))
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("checker did not stop after its context was cancelled")
	}
}
