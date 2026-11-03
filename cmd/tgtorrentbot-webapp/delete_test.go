package main

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/minya/tgtorrentbot/internal/media"
	"github.com/odwrtw/transmission"
)

// newDeleteTestApp returns an App whose Transmission RPC serves torrentsJSON
// for torrent-get and records torrent-remove calls, plus its download and
// incomplete roots.
func newDeleteTestApp(t *testing.T, torrentsJSON string) (app *App, root, inc string, removed *atomic.Bool) {
	t.Helper()
	removed = &atomic.Bool{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var req struct{ Method string }
		_ = json.Unmarshal(b, &req)
		if req.Method == "torrent-remove" {
			removed.Store(true)
			w.Write([]byte(`{"result":"success","arguments":{}}`))
			return
		}
		w.Write([]byte(`{"result":"success","arguments":{"torrents":` + torrentsJSON + `}}`))
	}))
	t.Cleanup(ts.Close)

	root = t.TempDir()
	inc = filepath.Join(root, "incomplete")
	tc, err := transmission.New(transmission.Config{Address: ts.URL})
	if err != nil {
		t.Fatal(err)
	}
	app = &App{
		config:               Config{DownloadPath: root, IncompletePath: inc},
		transmissionClient:   tc,
		jellyfinClient:       media.NewJellyfinClient("", ""),
		audiobookshelfClient: media.NewAudiobookshelfClient("", ""),
	}
	return app, root, inc, removed
}

func mustWriteFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func deleteItem(app *App, userID int64, category, name string) *httptest.ResponseRecorder {
	id := base64.URLEncoding.EncodeToString([]byte(category + ":" + name))
	rec := httptest.NewRecorder()
	app.handleItemDelete(userID, rec, httptest.NewRequest(http.MethodDelete, "/api/items/"+id, nil))
	return rec
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// An in-progress download lives in the incomplete dir but is shown under its
// real category; "Remove data" must delete it there.
func TestRemoveDataDeletesInProgressDownload(t *testing.T) {
	app, root, inc, removed := newDeleteTestApp(t,
		`[{"id":7,"name":"Foo","labels":["42","movies"],"percentDone":0.4}]`)
	mustWriteFile(t, filepath.Join(inc, "Foo", "part.mkv"))

	rec := deleteItem(app, 42, "movies", "Foo")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if !removed.Load() {
		t.Error("torrent was not removed")
	}
	if exists(filepath.Join(inc, "Foo")) {
		t.Error("in-progress data still exists in the incomplete dir")
	}
	if !exists(root) {
		t.Error("download root was removed")
	}
}

// A completed item lives under {DownloadPath}/{category}; that path must still
// be deleted, and a same-named folder of the incomplete dir with it.
func TestRemoveDataDeletesCompletedAndIncompleteCopies(t *testing.T) {
	app, root, inc, _ := newDeleteTestApp(t, `[]`)
	mustWriteFile(t, filepath.Join(root, "movies", "Bar", "movie.mkv"))
	mustWriteFile(t, filepath.Join(inc, "Bar", "leftover.part"))

	rec := deleteItem(app, 42, "movies", "Bar")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if exists(filepath.Join(root, "movies", "Bar")) {
		t.Error("completed data still exists")
	}
	if exists(filepath.Join(inc, "Bar")) {
		t.Error("incomplete copy still exists")
	}
	if !exists(filepath.Join(root, "movies")) {
		t.Error("category dir was removed")
	}
}

// The virtual incomplete category maps only to the incomplete dir.
func TestRemoveDataIncompleteCategory(t *testing.T) {
	app, _, inc, _ := newDeleteTestApp(t, `[]`)
	mustWriteFile(t, filepath.Join(inc, "Orphan", "x.part"))

	rec := deleteItem(app, 42, media.IncompleteCategory, "Orphan")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if exists(filepath.Join(inc, "Orphan")) {
		t.Error("orphaned incomplete data still exists")
	}
}

func TestRemoveDataNotFound(t *testing.T) {
	app, _, _, _ := newDeleteTestApp(t, `[]`)
	rec := deleteItem(app, 42, "movies", "Nothing")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// F83: a torrent the bot added from a group chat has the group as Labels[0]
// and the user as Labels[2]; it is listed for that user and deletable.
func TestGroupChatTorrentOwnedByUser(t *testing.T) {
	const torrents = `[{"id":9,"name":"Grp","labels":["-100123","movies","42"],"percentDone":1}]`
	app, root, _, removed := newDeleteTestApp(t, torrents)
	mustWriteFile(t, filepath.Join(root, "movies", "Grp", "a.mkv"))

	all, err := app.transmissionClient.GetTorrents()
	if err != nil {
		t.Fatal(err)
	}
	if got := userTorrents(all, 42); len(got) != 1 || got[0].Name != "Grp" {
		t.Fatalf("userTorrents(42) = %+v, want the group download", got)
	}
	if got := userTorrents(all, -100123); len(got) != 0 {
		t.Fatalf("userTorrents(group id) = %+v, want none", got)
	}

	rec := deleteItem(app, 42, "movies", "Grp")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if !removed.Load() {
		t.Error("torrent was not removed")
	}
}

// Torrents labelled before the owner label existed still belong to Labels[0].
func TestLegacyTwoLabelTorrentOwnedByLabel0(t *testing.T) {
	app, _, _, _ := newDeleteTestApp(t, `[{"id":1,"name":"Old","labels":["42","movies"]}]`)
	all, err := app.transmissionClient.GetTorrents()
	if err != nil {
		t.Fatal(err)
	}
	if got := userTorrents(all, 42); len(got) != 1 {
		t.Fatalf("userTorrents(42) = %+v, want the legacy torrent", got)
	}
}

// F39: user 42 cannot delete data that user 7's torrent still uses.
func TestRemoveDataRefusesOtherUsersTorrentData(t *testing.T) {
	app, root, _, removed := newDeleteTestApp(t,
		`[{"id":5,"name":"B Movie","labels":["7","movies","7"],"percentDone":1}]`)
	mustWriteFile(t, filepath.Join(root, "movies", "B Movie", "b.mkv"))

	rec := deleteItem(app, 42, "movies", "B Movie")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body %s", rec.Code, rec.Body.String())
	}
	if removed.Load() {
		t.Error("another user's torrent was removed")
	}
	if !exists(filepath.Join(root, "movies", "B Movie", "b.mkv")) {
		t.Error("another user's data was deleted")
	}
}

// Filesystem-only data (no torrent of anyone) stays deletable by any user.
func TestRemoveDataFilesystemOnlyIsShared(t *testing.T) {
	app, root, _, _ := newDeleteTestApp(t,
		`[{"id":5,"name":"Other Name","labels":["7","movies","7"],"percentDone":1}]`)
	mustWriteFile(t, filepath.Join(root, "movies", "Loose", "x.mkv"))

	if rec := deleteItem(app, 42, "movies", "Loose"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if exists(filepath.Join(root, "movies", "Loose")) {
		t.Error("filesystem-only data still exists")
	}
}

// F40: names with an ellipsis are ordinary names and can be deleted.
func TestRemoveDataNameWithDots(t *testing.T) {
	app, root, _, _ := newDeleteTestApp(t, `[]`)
	mustWriteFile(t, filepath.Join(root, "shows", "Wait... Season 1", "e01.mkv"))

	if rec := deleteItem(app, 42, "shows", "Wait... Season 1"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if exists(filepath.Join(root, "shows", "Wait... Season 1")) {
		t.Error("data still exists")
	}
}

func TestRemoveDataRejectsTraversalNames(t *testing.T) {
	app, root, _, _ := newDeleteTestApp(t, `[]`)
	mustWriteFile(t, filepath.Join(root, "keep.txt"))
	for _, name := range []string{"..", ".", "../movies", `..\x`, "a/b", ""} {
		if rec := deleteItem(app, 42, "movies", name); rec.Code != http.StatusBadRequest {
			t.Errorf("name %q: status = %d, want 400", name, rec.Code)
		}
	}
	if !exists(filepath.Join(root, "keep.txt")) || !exists(root) {
		t.Fatal("traversal deleted data outside the category")
	}
}

func deleteTorrentOnly(app *App, userID int64, category, name, query string) *httptest.ResponseRecorder {
	id := base64.URLEncoding.EncodeToString([]byte(category + ":" + name))
	rec := httptest.NewRecorder()
	app.handleItemDelete(userID, rec, httptest.NewRequest(http.MethodDelete, "/api/items/"+id+"/torrent"+query, nil))
	return rec
}

// F79: with two torrents of the same name, "Remove torrent" removes the one
// whose id the item carries.
func TestRemoveTorrentByIDAmongSameNamed(t *testing.T) {
	var removedIDs []int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var req struct {
			Method    string
			Arguments struct{ IDs []int }
		}
		_ = json.Unmarshal(b, &req)
		if req.Method == "torrent-remove" {
			removedIDs = append(removedIDs, req.Arguments.IDs...)
			w.Write([]byte(`{"result":"success","arguments":{}}`))
			return
		}
		w.Write([]byte(`{"result":"success","arguments":{"torrents":[` +
			`{"id":1,"name":"Movie","labels":["42","movies","42"]},` +
			`{"id":2,"name":"Movie","labels":["42","movies","42"]}]}}`))
	}))
	defer ts.Close()
	app, _, _, _ := newDeleteTestApp(t, `[]`)
	tc, _ := transmission.New(transmission.Config{Address: ts.URL})
	app.transmissionClient = tc

	if rec := deleteTorrentOnly(app, 42, "movies", "Movie", "?torrentId=2"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if len(removedIDs) != 1 || removedIDs[0] != 2 {
		t.Fatalf("removed %v, want only torrent 2", removedIDs)
	}

	// Remove data untracks every torrent using the folder.
	removedIDs = nil
	mustWriteFile(t, filepath.Join(app.config.DownloadPath, "movies", "Movie", "a.mkv"))
	if rec := deleteItem(app, 42, "movies", "Movie"); rec.Code != http.StatusOK {
		t.Fatalf("remove data: status = %d, body %s", rec.Code, rec.Body.String())
	}
	if len(removedIDs) != 2 {
		t.Fatalf("remove data removed %v, want both torrents", removedIDs)
	}
}
