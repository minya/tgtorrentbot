package media

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// cancelObservingServer never answers; it counts requests that arrive and
// requests whose client went away (context cancelled), as a hung Jellyfin or
// ABS would see them.
func cancelObservingServer(t *testing.T) (url string, started, cancelled *atomic.Int32) {
	t.Helper()
	started, cancelled = &atomic.Int32{}, &atomic.Int32{}
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started.Add(1)
		select {
		case <-r.Context().Done():
			cancelled.Add(1)
		case <-done:
		}
	}))
	t.Cleanup(func() { close(done); srv.Close() })
	return srv.URL, started, cancelled
}

// F43: when the caller's deadline passes, the requests to hung optional
// sources are aborted instead of running on until the 30 s client timeout.
func TestFetchLibrariesCancelsHungRequests(t *testing.T) {
	jfURL, jfStarted, jfCancelled := cancelObservingServer(t)
	absURL, absStarted, absCancelled := cancelObservingServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	jf, abs, errs := FetchLibraries(ctx, NewJellyfinClient(jfURL, "k"), NewAudiobookshelfClient(absURL, "k"))
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("FetchLibraries took %s after a 200 ms deadline", took)
	}
	if jf != nil || abs != nil {
		t.Fatalf("got items from hung servers: %v %v", jf, abs)
	}
	if len(errs) != 2 {
		t.Fatalf("errs = %v, want one per source", errs)
	}

	deadline := time.Now().Add(2 * time.Second)
	for (jfCancelled.Load() < jfStarted.Load() || absCancelled.Load() < absStarted.Load()) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if jfStarted.Load() != 1 || jfCancelled.Load() != 1 {
		t.Errorf("Jellyfin: %d requests, %d cancelled; want 1 and 1", jfStarted.Load(), jfCancelled.Load())
	}
	if absStarted.Load() != 1 || absCancelled.Load() != 1 {
		t.Errorf("Audiobookshelf: %d requests, %d cancelled; want 1 and 1", absStarted.Load(), absCancelled.Load())
	}
}

// Both sources are fetched at the same time, not one after the other.
func TestFetchLibrariesIsConcurrent(t *testing.T) {
	slow := func(body string) string {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(300 * time.Millisecond)
			w.Write([]byte(body))
		}))
		t.Cleanup(srv.Close)
		return srv.URL
	}
	jfURL := slow(`{"Items":[]}`)
	absURL := slow(`{"libraries":[]}`)
	start := time.Now()
	FetchLibraries(context.Background(), NewJellyfinClient(jfURL, "k"), NewAudiobookshelfClient(absURL, "k"))
	if took := time.Since(start); took > 550*time.Millisecond {
		t.Fatalf("took %s for two 300 ms sources; they were fetched sequentially", took)
	}
}

// F109: one failing Audiobookshelf library keeps the items of the others
// and is reported, instead of being dropped silently.
func TestFetchLibrariesReportsFailedABSLibrary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/libraries":
			w.Write([]byte(`{"libraries":[{"id":"ok","mediaType":"book"},{"id":"bad","mediaType":"book"}]}`))
		case "/api/libraries/ok/items":
			w.Write([]byte(`{"results":[{"id":"li_1","relPath":"Author - Book","path":"/audiobooks/Author - Book"}]}`))
		default:
			http.Error(w, "boom", http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	_, abs, errs := FetchLibraries(context.Background(), NewJellyfinClient("", ""), NewAudiobookshelfClient(srv.URL, "k"))
	if len(abs) != 1 {
		t.Fatalf("got %d ABS items, want the working library's 1", len(abs))
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "audiobookshelf partly unavailable") || !strings.Contains(errs[0].Error(), "library bad") {
		t.Fatalf("errs = %v, want the failed library reported", errs)
	}
}
