package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minya/tgtorrentbot/internal/media"
)

// hangingServer accepts requests and never answers until the test ends, like
// a Jellyfin/ABS that is starting up or a NAS that is asleep.
func hangingServer(t *testing.T) string {
	t.Helper()
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-done:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(done); srv.Close() })
	return srv.URL
}

func withOptionalTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	old := optionalSourceTimeout
	optionalSourceTimeout = d
	t.Cleanup(func() { optionalSourceTimeout = old })
}

// F36: /api/items answers from Transmission and the filesystem when the
// optional sources hang.
func TestItemsAnswerWhenOptionalSourcesHang(t *testing.T) {
	withOptionalTimeout(t, 200*time.Millisecond)
	app, root, _, _ := newDeleteTestApp(t, `[{"id":1,"name":"Foo","labels":["42","movies"],"percentDone":1}]`)
	mustWriteFile(t, filepath.Join(root, "movies", "Foo", "a.mkv"))
	hang := hangingServer(t)
	app.jellyfinClient = media.NewJellyfinClient(hang, "k")
	app.audiobookshelfClient = media.NewAudiobookshelfClient(hang, "k")

	start := time.Now()
	rec := httptest.NewRecorder()
	app.handleUnifiedItems(42, rec, httptest.NewRequest(http.MethodGet, "/api/items", nil))
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("/api/items took %s with hanging optional sources", took)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, `"Foo"`) {
		t.Fatalf("body lacks the torrent/filesystem item: %s", body)
	}
}

// F36: "Remove data" answers success right after deleting, without waiting
// for the library refreshes.
func TestRemoveDataDoesNotWaitForRefresh(t *testing.T) {
	app, root, _, _ := newDeleteTestApp(t, `[]`)
	mustWriteFile(t, filepath.Join(root, "movies", "Bar", "a.mkv"))
	hang := hangingServer(t)
	app.jellyfinClient = media.NewJellyfinClient(hang, "k")
	app.audiobookshelfClient = media.NewAudiobookshelfClient(hang, "k")

	start := time.Now()
	rec := deleteItem(app, 42, "movies", "Bar")
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("Remove data took %s with hanging refreshes", took)
	}
	if rec.Code != http.StatusOK || exists(filepath.Join(root, "movies", "Bar")) {
		t.Fatalf("status = %d, data exists = %v", rec.Code, exists(filepath.Join(root, "movies", "Bar")))
	}
}
