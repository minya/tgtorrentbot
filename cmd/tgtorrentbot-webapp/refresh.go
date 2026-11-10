package main

import (
	"context"
	"sync"
)

// libraryRefresher runs the Jellyfin and Audiobookshelf refreshes that
// follow a deletion, in the background and one at a time. A request made
// while one runs queues a single further run, so a series of deletions
// costs two rescans, not one each. Wait lets shutdown finish the run in
// progress. The zero value is ready to use.
type libraryRefresher struct {
	mu      sync.Mutex
	running bool
	pending bool
	// done is closed when the current series of runs ends.
	done chan struct{}
}

// trigger runs refresh in the background, or queues one more run if one is
// in progress.
func (r *libraryRefresher) trigger(refresh func(context.Context)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		r.pending = true
		return
	}
	r.running = true
	r.done = make(chan struct{})
	go r.loop(refresh, r.done)
}

func (r *libraryRefresher) loop(refresh func(context.Context), done chan struct{}) {
	defer close(done)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
		refresh(ctx)
		cancel()
		r.mu.Lock()
		if !r.pending {
			r.running = false
			r.mu.Unlock()
			return
		}
		r.pending = false
		r.mu.Unlock()
	}
}

// Wait blocks until no refresh runs or ctx is done.
func (r *libraryRefresher) Wait(ctx context.Context) error {
	r.mu.Lock()
	running, done := r.running, r.done
	r.mu.Unlock()
	if !running {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
