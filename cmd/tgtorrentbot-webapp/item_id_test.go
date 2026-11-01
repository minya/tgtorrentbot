package main

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/minya/tgtorrentbot/internal/media"
	"github.com/odwrtw/transmission"
)

func TestDecodeItemID(t *testing.T) {
	enc := func(s string) string { return base64.URLEncoding.EncodeToString([]byte(s)) }
	raw := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	for name, tc := range map[string]struct {
		in, cat, item string
		ok            bool
	}{
		"padded":         {enc("movies:Foo"), "movies", "Foo", true},
		"unpadded":       {raw("movies:Foo Bar"), "movies", "Foo Bar", true},
		"colon in name":  {enc("shows:A: B"), "shows", "A: B", true},
		"cyrillic":       {enc("music:Кино"), "music", "Кино", true},
		"no separator":   {enc("moviesFoo"), "", "", false},
		"empty name":     {enc("movies:"), "", "", false},
		"empty category": {enc(":Foo"), "", "", false},
		"not base64":     {"%%%", "", "", false},
		"invalid utf-8":  {enc("movies:\xff\xfe"), "", "", false},
	} {
		t.Run(name, func(t *testing.T) {
			cat, item, err := decodeItemID(tc.in)
			if (err == nil) != tc.ok || cat != tc.cat || item != tc.item {
				t.Fatalf("decodeItemID = %q, %q, %v", cat, item, err)
			}
		})
	}
}

// The delete guard refuses names and categories that could leave the
// category directory, and deletes nothing.
func TestItemDeleteRejectsTraversal(t *testing.T) {
	app, root, inc, removed := newDeleteTestApp(t, `[]`)
	mustWriteFile(t, filepath.Join(root, "secret.txt"))
	mustWriteFile(t, filepath.Join(root, "movies", "Keep", "a.mkv"))
	mustWriteFile(t, filepath.Join(inc, "Keep", "b.part"))

	for name, id := range map[string]string{
		"dotdot name":        "movies:..",
		"dotdot path":        "movies:../secret.txt",
		"slash in name":      "movies:Keep/a.mkv",
		"backslash in name":  `movies:Keep\a.mkv`,
		"unknown category":   "secret:Keep",
		"traversal category": "../..:Keep",
	} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			app.handleItemDelete(42, rec, httptest.NewRequest(http.MethodDelete,
				"/api/items/"+base64.URLEncoding.EncodeToString([]byte(id)), nil))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status %d, want 400", rec.Code)
			}
		})
	}
	for _, p := range []string{filepath.Join(root, "secret.txt"), filepath.Join(root, "movies", "Keep"), filepath.Join(inc, "Keep")} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s was deleted", p)
		}
	}
	if removed.Load() {
		t.Error("a torrent was removed")
	}
}

func TestUserTorrentsFiltersByOwner(t *testing.T) {
	ts := []*transmission.Torrent{
		{ID: 1, Name: "mine", Labels: []string{"42", "movies"}, PercentDone: 0.5},
		{ID: 2, Name: "theirs", Labels: []string{"7", "movies"}},
		{ID: 3, Name: "legacy", Labels: []string{"42"}},
		{ID: 4, Name: "unlabelled"},
	}
	got := userTorrents(ts, 42)
	if len(got) != 2 || got[0].ID != 1 || got[1].ID != 3 {
		t.Fatalf("userTorrents = %+v", got)
	}
	if got[0].PercentDone != 50 || got[0].Category != "movies" || got[1].Category != "others" {
		t.Fatalf("converted fields wrong: %+v", got)
	}
}

// WebSocket updates: active torrents are always sent, a completion is sent
// once when progress reaches 100%, and finished torrents are not resent.
func TestTorrentUpdatesCompletion(t *testing.T) {
	prev := map[int]float64{}
	ti := func(pct float64) []media.TorrentInfo {
		return []media.TorrentInfo{{ID: 1, Name: "X", PercentDone: pct}}
	}

	if u := torrentUpdates(ti(40), prev); len(u) != 1 || u[0].IsComplete {
		t.Fatalf("downloading: %+v", u)
	}
	if u := torrentUpdates(ti(100), prev); len(u) != 1 || !u[0].IsComplete {
		t.Fatalf("just completed: %+v", u)
	}
	if u := torrentUpdates(ti(100), prev); len(u) != 0 {
		t.Fatalf("already complete was resent: %+v", u)
	}
	// First seen already complete: not announced as a fresh completion.
	if u := torrentUpdates([]media.TorrentInfo{{ID: 2, PercentDone: 100}}, prev); len(u) != 0 {
		t.Fatalf("first seen complete: %+v", u)
	}
}

func TestPrunePercent(t *testing.T) {
	prev := map[int]float64{1: 50, 2: 100}
	prunePercent(prev, []*transmission.Torrent{{ID: 1}})
	if _, ok := prev[2]; ok || len(prev) != 1 {
		t.Fatalf("prev = %v", prev)
	}
}
