package rutrackersession

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/minya/rutracker"
)

func counting(fail *atomic.Bool, logins *atomic.Int32) *Session {
	return &Session{newClient: func(context.Context) (*rutracker.RutrackerClient, error) {
		logins.Add(1)
		if fail.Load() {
			return nil, rutracker.ErrLoginFailed
		}
		return &rutracker.RutrackerClient{}, nil
	}}
}

// F20: many searches and downloads, even concurrent ones, share one login.
func TestSessionLogsInOnce(t *testing.T) {
	var logins atomic.Int32
	s := counting(new(atomic.Bool), &logins)
	var wg sync.WaitGroup
	clients := make([]*rutracker.RutrackerClient, 20)
	for i := range clients {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := s.Client(context.Background())
			if err != nil {
				t.Error(err)
			}
			clients[i] = c
		}()
	}
	wg.Wait()
	if logins.Load() != 1 {
		t.Fatalf("logins = %d, want 1", logins.Load())
	}
	for _, c := range clients {
		if c != clients[0] {
			t.Fatal("callers got different clients")
		}
	}
}

// A failed login is retried on the next request, not cached.
func TestSessionRetriesFailedLogin(t *testing.T) {
	var logins atomic.Int32
	fail := new(atomic.Bool)
	fail.Store(true)
	s := counting(fail, &logins)
	if _, err := s.Client(context.Background()); !errors.Is(err, rutracker.ErrLoginFailed) {
		t.Fatalf("err = %v, want ErrLoginFailed", err)
	}
	fail.Store(false)
	if _, err := s.Client(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Client(context.Background()); err != nil {
		t.Fatal(err)
	}
	if logins.Load() != 2 {
		t.Fatalf("logins = %d, want 2 (one failed, one kept)", logins.Load())
	}
}
