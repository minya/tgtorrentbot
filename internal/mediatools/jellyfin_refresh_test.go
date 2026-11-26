package mediatools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/minya/tgtorrentbot/internal/media"
)

// fakeJellyfin lists the albums of the АК-47 case and records refreshes.
type fakeJellyfin struct {
	mu          sync.Mutex
	refreshed   []string // item ids, with their query
	libraryScan int
	failRefresh bool
}

func (f *fakeJellyfin) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/Items":
		w.Write([]byte(`{"Items":[
			{"Name":"Новый","Id":"new","Path":"/media/music/АК-47 - Дискография (2009-2017) MP3/2017 - Новый"},
			{"Name":"Третий","Id":"third","Path":"/media/music/АК-47 - Дискография (2009-2017) MP3/2015 - Третий"},
			{"Name":"АК-47 Неизданное","Id":"unreleased","Path":"/media/music/AK-47"},
			{"Name":"AK-47 Live","Id":"prefix","Path":"/media/music/AK-47 Live"}
		]}`))
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/Items/") && strings.HasSuffix(r.URL.Path, "/Refresh"):
		if f.failRefresh {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/Items/"), "/Refresh")
		f.refreshed = append(f.refreshed, id+"?"+r.URL.RawQuery)
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && r.URL.Path == "/Library/Refresh":
		f.libraryScan++
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func jellyfinTools(t *testing.T, fake *fakeJellyfin) *Tools {
	t.Helper()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	root := t.TempDir()
	for _, d := range []string{
		"music/АК-47 - Дискография (2009-2017) MP3/2017 - Новый",
		"music/АК-47 - Дискография (2009-2017) MP3/2015 - Третий",
		"music/AK-47",
		"music/AK-47 Live",
		"music/Unscanned",
	} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(root, "music/AK-47/01.mp3"), nil, 0o644)
	return &Tools{config: Config{DownloadPath: root, JellyfinURL: srv.URL, JellyfinAPIKey: "k"}, jf: media.NewJellyfinClient(srv.URL, "k")}
}

// F7: a library scan keeps a track's album artists, so after write_tags the
// albums at the path are refreshed with replaceAllMetadata.
func TestJellyfinRescanPathRefreshesItems(t *testing.T) {
	for _, tc := range []struct {
		path string
		want []string
	}{
		{"music/АК-47 - Дискография (2009-2017) MP3/2017 - Новый", []string{"new"}},
		{"music/АК-47 - Дискография (2009-2017) MP3", []string{"new", "third"}}, // albums inside
		{"music/AK-47/01.mp3", []string{"unreleased"}},                         // the album around a file
		{"music/AK-47", []string{"unreleased"}},                                // not "AK-47 Live"
	} {
		fake := &fakeJellyfin{}
		s := jellyfinTools(t, fake)
		res, out, _ := s.jellyfinRescan(context.Background(), nil, jellyfinRescanInput{Path: tc.path})
		if res.IsError || !out.Triggered || out.LibraryScan || fake.libraryScan != 0 {
			t.Errorf("%s: %+v %+v, library scans %d", tc.path, res.Content, out, fake.libraryScan)
			continue
		}
		var ids []string
		for _, r := range fake.refreshed {
			id, q, _ := strings.Cut(r, "?")
			ids = append(ids, id)
			for _, p := range []string{"metadataRefreshMode=FullRefresh", "replaceAllMetadata=true", "replaceAllImages=false"} {
				if !strings.Contains(q, p) {
					t.Errorf("%s: refresh of %s without %s: %s", tc.path, id, p, q)
				}
			}
		}
		sort.Strings(ids)
		if strings.Join(ids, ",") != strings.Join(tc.want, ",") {
			t.Errorf("%s: refreshed %v, want %v", tc.path, ids, tc.want)
		}
	}
}

func TestJellyfinRescanPathNotInJellyfinScans(t *testing.T) {
	fake := &fakeJellyfin{}
	s := jellyfinTools(t, fake)
	res, out, _ := s.jellyfinRescan(context.Background(), nil, jellyfinRescanInput{Path: "music/Unscanned"})
	if res.IsError || !out.LibraryScan || fake.libraryScan != 1 || len(fake.refreshed) != 0 {
		t.Errorf("%+v %+v, scans %d, refreshed %v", res.Content, out, fake.libraryScan, fake.refreshed)
	}
}

func TestJellyfinRescanPathRefused(t *testing.T) {
	for _, p := range []string{"music", ".", "../etc", "music/missing"} {
		fake := &fakeJellyfin{}
		s := jellyfinTools(t, fake)
		res, _, _ := s.jellyfinRescan(context.Background(), nil, jellyfinRescanInput{Path: p})
		if !res.IsError || len(fake.refreshed) != 0 || fake.libraryScan != 0 {
			t.Errorf("%q: want a refusal without requests, got %+v", p, res.Content)
		}
	}
}

func TestJellyfinRescanPathRefreshFails(t *testing.T) {
	fake := &fakeJellyfin{failRefresh: true}
	s := jellyfinTools(t, fake)
	if res, out, _ := s.jellyfinRescan(context.Background(), nil, jellyfinRescanInput{Path: "music/AK-47"}); !res.IsError || out.Triggered {
		t.Errorf("failed refresh reported as done: %+v", out)
	}
}
