package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minya/telegram"
)

const forgedUpdate = `{"update_id":1,"message":{"message_id":1,"from":{"id":42},"chat":{"id":42},"text":"/remove 5"}}`

func postUpdate(h http.Handler, secret *string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(forgedUpdate))
	if secret != nil {
		req.Header.Set("X-Telegram-Bot-Api-Secret-Token", *secret)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestWebhookRejectsRequestsWithoutSecret(t *testing.T) {
	secret := deriveWebhookSecret("123:token")
	handled := make(chan *telegram.Update, 1)
	h := newWebhookHandler(secret, func(_ context.Context, u *telegram.Update) error {
		handled <- u
		return nil
	}, new(sync.WaitGroup))

	wrong := "nope"
	for name, hdr := range map[string]*string{"missing": nil, "wrong": &wrong} {
		if rec := postUpdate(h, hdr); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s secret: status %d, want 401", name, rec.Code)
		}
	}
	select {
	case <-handled:
		t.Fatal("an update without the right secret reached the handler")
	case <-time.After(100 * time.Millisecond):
	}

	if rec := postUpdate(h, &secret); rec.Code != http.StatusOK {
		t.Fatalf("right secret: status %d, want 200", rec.Code)
	}
	select {
	case u := <-handled:
		if u.Message == nil || u.Message.Text != "/remove 5" {
			t.Fatalf("handler got %+v", u)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("update with the right secret was not handled")
	}
}

func TestWebhookRejectsGet(t *testing.T) {
	secret := deriveWebhookSecret("123:token")
	h := newWebhookHandler(secret, func(context.Context, *telegram.Update) error { return nil }, new(sync.WaitGroup))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", secret)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d, want 405", rec.Code)
	}
}

func TestDerivedWebhookSecret(t *testing.T) {
	a, b := deriveWebhookSecret("123:a"), deriveWebhookSecret("123:b")
	if !validWebhookSecret(a) {
		t.Fatalf("derived secret %q is not a valid secret_token", a)
	}
	if a == b {
		t.Fatal("different bot tokens give the same secret")
	}
	if strings.Contains(a, "123") {
		t.Fatal("derived secret leaks the bot token")
	}
}

func TestValidWebhookSecret(t *testing.T) {
	for s, want := range map[string]bool{
		"abc_DEF-123":            true,
		"":                       false,
		"has space":              false,
		"semi;colon":             false,
		strings.Repeat("a", 256): true,
		strings.Repeat("a", 257): false,
	} {
		if got := validWebhookSecret(s); got != want {
			t.Errorf("validWebhookSecret(%q) = %v, want %v", s, got, want)
		}
	}
}

// F44: every update handled in the background is counted, so shutdown can
// wait for it.
func TestWebhookTracksInFlightUpdates(t *testing.T) {
	secret := deriveWebhookSecret("123:token")
	release := make(chan struct{})
	var inFlight sync.WaitGroup
	h := newWebhookHandler(secret, func(context.Context, *telegram.Update) error {
		<-release
		return nil
	}, &inFlight)
	if rec := postUpdate(h, &secret); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	waitGroup(ctx, &inFlight)
	if time.Since(start) < 90*time.Millisecond {
		t.Fatal("waitGroup returned while an update was still being handled")
	}
	close(release)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	waitGroup(ctx2, &inFlight)
	if ctx2.Err() != nil {
		t.Fatal("waitGroup did not return after the update finished")
	}
}
