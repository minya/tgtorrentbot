package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type hungWebhook struct{ finishes bool }

func (h hungWebhook) Wait(ctx context.Context) error {
	if h.finishes {
		return nil
	}
	<-ctx.Done()
	return ctx.Err()
}

func closedChan() <-chan struct{} { c := make(chan struct{}); close(c); return c }

// F112: an update still being handled when shutdown runs out of time gets
// a reply telling its chat to send it again, before the deadline; one chat
// with two updates gets one notice.
func TestDrainTellsInterruptedChats(t *testing.T) {
	var sent atomic.Int32
	api := fakeTelegram(t, &sent)
	var inFlight inFlightChats
	inFlight.track(5)
	inFlight.track(5)
	inFlight.track(6)

	ctx, cancel := context.WithTimeout(context.Background(), noticeBudget+300*time.Millisecond)
	defer cancel()
	drain(ctx, hungWebhook{}, closedChan(), &inFlight, api)
	if ctx.Err() != nil {
		t.Fatal("drain ran past the shutdown deadline")
	}
	if sent.Load() != 2 {
		t.Fatalf("sent %d notices, want one per chat (2)", sent.Load())
	}
}

func TestDrainSilentWhenUpdatesFinish(t *testing.T) {
	var sent atomic.Int32
	api := fakeTelegram(t, &sent)
	var inFlight inFlightChats
	done := inFlight.track(5)
	done()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	drain(ctx, hungWebhook{finishes: true}, closedChan(), &inFlight, api)
	if sent.Load() != 0 || len(inFlight.list()) != 0 {
		t.Fatalf("sent %d notices, %d chats left", sent.Load(), len(inFlight.list()))
	}
}
