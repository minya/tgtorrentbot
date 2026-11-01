package transmissionrpc

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeRPC answers 409 with a session id until the request carries it, and
// rotates the id after every rotateEvery successful requests, like a
// Transmission restart. It reports the total number of requests.
func fakeRPC(t *testing.T, rotateEvery int64) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var n atomic.Int64
	var mu sync.Mutex
	var ok, gen int64
	id := "s0"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		mu.Lock()
		cur := id
		if r.Header.Get(sessionHeader) != cur {
			mu.Unlock()
			w.Header().Set(sessionHeader, cur)
			w.WriteHeader(http.StatusConflict)
			return
		}
		if ok++; rotateEvery > 0 && ok%rotateEvery == 0 {
			gen++
			id = fmt.Sprintf("s%d", gen)
		}
		mu.Unlock()
		w.Write([]byte(`{"result":"success","arguments":{"torrents":[{"id":1,"name":"X"}]}}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

// Concurrent calls across session-id rotations must all succeed; run with
// -race, which flagged the library's own session handling (F22).
func TestConcurrentCallsWithSessionRotation(t *testing.T) {
	srv, _ := fakeRPC(t, 40)
	c, err := New(srv.URL, "u", "p")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var failures atomic.Int32
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 10 {
				ts, err := c.GetTorrents()
				if err != nil || len(ts) != 1 {
					failures.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	if f := failures.Load(); f != 0 {
		t.Fatalf("%d of 160 calls failed", f)
	}
}

func TestSessionIDReusedAfterFirst409(t *testing.T) {
	srv, n := fakeRPC(t, 0)
	c, _ := New(srv.URL, "", "")
	for range 5 {
		if _, err := c.GetTorrents(); err != nil {
			t.Fatal(err)
		}
	}
	// One 409 for the first call, then every call goes straight through.
	if got := n.Load(); got != 6 {
		t.Fatalf("server saw %d requests for 5 calls, want 6", got)
	}
}

// A stalled Transmission must not block the caller forever (F21).
func TestCallsTimeOut(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer srv.Close()
	defer close(release)

	c, _ := newWithTimeout(srv.URL, "", "", 200*time.Millisecond)
	start := time.Now()
	_, err := c.GetTorrents()
	if err == nil {
		t.Fatal("call against a stalled server succeeded")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("call took %v, want about the 200ms timeout", d)
	}
}

func TestNewSetsDefaultTimeout(t *testing.T) {
	c, _ := New("http://127.0.0.1:1", "", "")
	if c.HTTPClient.Timeout != DefaultTimeout {
		t.Fatalf("timeout = %v, want %v", c.HTTPClient.Timeout, DefaultTimeout)
	}
}

// A 409 is never handed back to the library, even when Transmission keeps
// rejecting the session id.
func TestPersistent409BecomesError(t *testing.T) {
	var n atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(sessionHeader, fmt.Sprintf("id%d", n.Add(1)))
		w.WriteHeader(http.StatusConflict)
	}))
	defer srv.Close()
	c, _ := New(srv.URL, "", "")
	if _, err := c.GetTorrents(); err == nil {
		t.Fatal("expected an error")
	}
	if got := n.Load(); got != maxSessionRetries {
		t.Fatalf("server saw %d requests, want %d", got, maxSessionRetries)
	}
}
