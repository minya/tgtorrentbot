package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// F124: deletions in a row run one refresh plus one queued run, not one
// each, and Wait lets shutdown wait for the run in progress.
func TestLibraryRefresherCoalescesAndWaits(t *testing.T) {
	var r libraryRefresher
	var runs, active, maxActive atomic.Int32
	release := make(chan struct{})
	refresh := func(context.Context) {
		n := active.Add(1)
		if n > maxActive.Load() {
			maxActive.Store(n)
		}
		runs.Add(1)
		<-release
		active.Add(-1)
	}
	for range 10 {
		r.trigger(refresh)
	}
	short, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if r.Wait(short) == nil {
		t.Fatal("Wait returned while a refresh was running")
	}
	close(release)
	ctx, cancel2 := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel2()
	if err := r.Wait(ctx); err != nil {
		t.Fatal("refreshes did not finish")
	}
	if runs.Load() != 2 || maxActive.Load() != 1 {
		t.Fatalf("runs %d (want 2), at most %d at once (want 1)", runs.Load(), maxActive.Load())
	}
	// Idle again: a later deletion starts a new run.
	r.trigger(refresh)
	if err := r.Wait(ctx); err != nil || runs.Load() != 3 {
		t.Fatalf("after idle: runs %d, %v", runs.Load(), err)
	}
}
