package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/commands"
	"github.com/minya/tgtorrentbot/environment"
	"github.com/minya/tgtorrentbot/internal"
	"github.com/odwrtw/transmission"
)

// recordingTelegram is a fake Bot API that records the methods called.
type recordingTelegram struct {
	mu      sync.Mutex
	methods []string
}

func (r *recordingTelegram) calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.methods...)
}

func newRecordingTelegram(t *testing.T) (*recordingTelegram, *telegram.API) {
	t.Helper()
	rec := &recordingTelegram{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.methods = append(rec.methods, r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:])
		rec.mu.Unlock()
		w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	t.Cleanup(srv.Close)
	return rec, telegram.NewAPI("tok", telegram.WithBaseURL(srv.URL))
}

// stubFactory accepts every update and runs handle.
type stubFactory struct {
	accepted atomic.Int32
	handle   func(context.Context, *telegram.Update) error
}

func (f *stubFactory) Accepts(*telegram.Update) (bool, commands.Command) {
	f.accepted.Add(1)
	return true, f
}

func (f *stubFactory) Handle(ctx context.Context, upd *telegram.Update) error {
	if f.handle != nil {
		return f.handle(ctx, upd)
	}
	return nil
}

func newTestHandler(t *testing.T, allowed ...int64) (*UpdatesHandler, *stubFactory, *recordingTelegram, *atomic.Int32) {
	t.Helper()
	tg, api := newRecordingTelegram(t)
	var notified atomic.Int32
	h := NewUpdatesHandler(environment.Env{TgApi: api, AllowedUsers: allowed}, func() { notified.Add(1) })
	stub := &stubFactory{}
	h.commandsList = []commands.CommandFactory{stub}
	return h, stub, tg, &notified
}

func TestHandleUpdateAllowlist(t *testing.T) {
	msg := func(from *telegram.User) *telegram.Update {
		return &telegram.Update{Message: &telegram.Message{MessageID: 1, From: from, Text: "/list"}}
	}
	cb := func(from *telegram.User) *telegram.Update {
		return &telegram.Update{CallbackQuery: &telegram.CallbackQuery{ID: "q", From: from, Data: "/list_page 2",
			Message: &telegram.Message{MessageID: 1}}}
	}
	for name, tc := range map[string]struct {
		upd    *telegram.Update
		wantOK bool
	}{
		"allowed message":      {msg(&telegram.User{ID: 42}), true},
		"allowed callback":     {cb(&telegram.User{ID: 42}), true},
		"unknown message":      {msg(&telegram.User{ID: 7}), false},
		"unknown callback":     {cb(&telegram.User{ID: 7}), false},
		"message without from": {msg(nil), false},
		"empty update":         {&telegram.Update{}, false},
	} {
		t.Run(name, func(t *testing.T) {
			h, stub, tg, notified := newTestHandler(t, 42)
			if err := h.HandleUpdate(context.Background(), tc.upd); err != nil {
				t.Fatal(err)
			}
			if got := stub.accepted.Load() == 1; got != tc.wantOK {
				t.Fatalf("command reached = %v, want %v", got, tc.wantOK)
			}
			if !tc.wantOK && len(tg.calls()) != 0 {
				t.Fatalf("rejected update caused Bot API calls: %v", tg.calls())
			}
			if tc.wantOK && notified.Load() != 1 {
				t.Fatal("successful command did not trigger a completion check")
			}
		})
	}
}

func TestHandleUpdateErrorSkipsNotify(t *testing.T) {
	h, stub, _, notified := newTestHandler(t, 42)
	stub.handle = func(context.Context, *telegram.Update) error { return errors.New("boom") }
	err := h.HandleUpdate(context.Background(), &telegram.Update{Message: &telegram.Message{MessageID: 1, From: &telegram.User{ID: 42}}})
	if err == nil || notified.Load() != 0 {
		t.Fatalf("err = %v, notified = %d", err, notified.Load())
	}
}

// F14: a callback without a message (inline-mode button, or a malformed
// update) is answered and dropped before any command dereferences it.
func TestHandleUpdateCallbackWithoutMessage(t *testing.T) {
	h, stub, tg, _ := newTestHandler(t, 42)
	upd := &telegram.Update{CallbackQuery: &telegram.CallbackQuery{ID: "q", From: &telegram.User{ID: 42}, Data: "/dlmagnet movies x"}}
	if err := h.HandleUpdate(context.Background(), upd); err != nil {
		t.Fatal(err)
	}
	if stub.accepted.Load() != 0 {
		t.Fatal("a callback without a message reached a command")
	}
	if calls := tg.calls(); len(calls) != 1 || calls[0] != "answerCallbackQuery" {
		t.Fatalf("Bot API calls = %v, want [answerCallbackQuery]", calls)
	}
}

// F14 with the real commands: none of them may panic on such a callback.
func TestRealCommandsSurviveCallbackWithoutMessage(t *testing.T) {
	_, api := newRecordingTelegram(t)
	h := NewUpdatesHandler(environment.Env{TgApi: api, AllowedUsers: []int64{42}}, func() {})
	for _, data := range []string{"/dl dl.php?t=1", "/dlcat movies dl.php?t=1", "/dlmagnet movies x", "/dlfilecat movies x", "/list_page 2"} {
		upd := &telegram.Update{CallbackQuery: &telegram.CallbackQuery{ID: "q", From: &telegram.User{ID: 42}, Data: data}}
		if err := h.HandleUpdate(context.Background(), upd); err != nil {
			t.Errorf("%s: %v", data, err)
		}
	}
}

// A panic inside a command is recovered in the webhook goroutine instead of
// killing the process.
func TestWebhookRecoversPanic(t *testing.T) {
	secret := deriveWebhookSecret("123:token")
	done := make(chan struct{})
	h := newWebhookHandler(secret, func(context.Context, *telegram.Update) error {
		defer close(done)
		var m map[string]int
		m["x"] = 1 // nil map write panics
		return nil
	})
	if rec := postUpdate(h, &secret); rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not run")
	}
	time.Sleep(50 * time.Millisecond) // the test binary would have crashed by now
}

// F41: a command error the command did not report is answered in the chat.
func TestHandleUpdateErrorRepliesInChat(t *testing.T) {
	h, stub, tg, _ := newTestHandler(t, 42)
	stub.handle = func(context.Context, *telegram.Update) error { return errors.New("rutracker unreachable") }
	_ = h.HandleUpdate(context.Background(), &telegram.Update{Message: &telegram.Message{MessageID: 1, From: &telegram.User{ID: 42}, Chat: telegram.Chat{ID: 42}}})
	if got := tg.calls(); !slices.Contains(got, "sendMessage") {
		t.Fatalf("Telegram calls = %v, want a sendMessage", got)
	}
}

// An error the command already reported is not answered twice.
func TestHandleUpdateRepliedErrorNotRepeated(t *testing.T) {
	h, stub, tg, _ := newTestHandler(t, 42)
	stub.handle = func(context.Context, *telegram.Update) error { return commands.Replied(errors.New("told already")) }
	_ = h.HandleUpdate(context.Background(), &telegram.Update{Message: &telegram.Message{MessageID: 1, From: &telegram.User{ID: 42}, Chat: telegram.Chat{ID: 42}}})
	if got := tg.calls(); slices.Contains(got, "sendMessage") {
		t.Fatalf("Telegram calls = %v, want no second message", got)
	}
}

func TestFailureTextDuplicate(t *testing.T) {
	if got := failureText(fmt.Errorf("add: %w", transmission.ErrDuplicateTorrent)); !strings.Contains(got, "already") {
		t.Fatalf("failureText(duplicate) = %q", got)
	}
}

// F50: a button press no command accepts, and one whose category no longer
// exists, are answered so the client's spinner stops.
func TestUnmatchedCallbacksAreAnswered(t *testing.T) {
	for _, data := range []string{"/nosuchcommand 1", "/dlcat bogus dl.php?t=1", "/dlmagnet bogus abc", "/dlfilecat bogus abc"} {
		tg, api := newRecordingTelegram(t)
		h := NewUpdatesHandler(environment.Env{TgApi: api, AllowedUsers: []int64{42}, FileIDLookup: &internal.FileIDLookup{}}, func() {})
		_ = h.HandleUpdate(context.Background(), &telegram.Update{CallbackQuery: &telegram.CallbackQuery{
			ID: "q", Data: data, From: &telegram.User{ID: 42},
			Message: &telegram.Message{MessageID: 1, Chat: telegram.Chat{ID: 42}},
		}})
		calls := tg.calls()
		if !slices.Contains(calls, "answerCallbackQuery") {
			t.Errorf("%q: Telegram calls %v, want answerCallbackQuery", data, calls)
		}
		if !slices.Contains(calls, "sendMessage") {
			t.Errorf("%q: Telegram calls %v, want a message explaining it", data, calls)
		}
	}
}

func widgetQuery(from int64, data string) *telegram.Update {
	return &telegram.Update{WidgetQuery: &telegram.WidgetQuery{
		ID: "wq", From: &telegram.User{ID: from}, Chat: &telegram.Chat{ID: from, Type: "private"}, MessageID: 5, Data: data,
	}}
}

// A progress widget asks every 5 s while on screen: it must reach its
// command only for allowed users and never trigger a completion check.
func TestWidgetQueryAllowlistAndNoNotify(t *testing.T) {
	h, stub, tg, notified := newTestHandler(t, 42)
	if err := h.HandleUpdate(context.Background(), widgetQuery(7, "p:x")); err != nil {
		t.Fatal(err)
	}
	if stub.accepted.Load() != 0 || len(tg.calls()) != 0 {
		t.Fatalf("unknown user's widget query reached a command or Telegram: %v", tg.calls())
	}
	if err := h.HandleUpdate(context.Background(), widgetQuery(42, "p:x")); err != nil {
		t.Fatal(err)
	}
	if stub.accepted.Load() != 1 || notified.Load() != 0 {
		t.Fatalf("accepted = %d, notified = %d", stub.accepted.Load(), notified.Load())
	}
}

// An unknown widget query is answered at once (not left to time out after
// 15 s) and gets no chat message.
func TestUnmatchedWidgetQueryIsAnswered(t *testing.T) {
	tg, api := newRecordingTelegram(t)
	h := NewUpdatesHandler(environment.Env{TgApi: api, AllowedUsers: []int64{42}, FileIDLookup: &internal.FileIDLookup{}}, func() {})
	if err := h.HandleUpdate(context.Background(), widgetQuery(42, "nonsense")); err != nil {
		t.Fatal(err)
	}
	if calls := tg.calls(); !slices.Equal(calls, []string{"answerWidgetQuery"}) {
		t.Fatalf("Telegram calls %v, want only answerWidgetQuery", calls)
	}
}

func TestWidgetQueriesAreNotOrderedBehindTheChat(t *testing.T) {
	if _, ok := updateOrderKey(widgetQuery(42, "p:x")); ok {
		t.Error("a widget query must not wait behind the chat's other updates")
	}
	if key, ok := updateOrderKey(&telegram.Update{Message: &telegram.Message{Chat: telegram.Chat{ID: 42}}}); !ok || key != 42 {
		t.Errorf("messages must still be ordered per chat, got %d, %v", key, ok)
	}
}
