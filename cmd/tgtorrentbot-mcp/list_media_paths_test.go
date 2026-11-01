package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/odwrtw/transmission"
)

// list_media must report where the data really is: the incomplete directory
// for in-progress downloads and orphans, even when TGT_INCOMPLETE_PATH is not
// under the download root.
func TestListMediaPathsForIncompleteData(t *testing.T) {
	s, root := newListServer(t)
	inc, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.config.IncompletePath = inc
	s.scan.IncompletePath = inc
	s.trans = &fakeTorrents{torrents: []*transmission.Torrent{
		{ID: 3, Name: "Foo", PercentDone: 0.4, Labels: []string{"42", "movies"}},
	}}
	writeFile(t, filepath.Join(inc, "Foo", "part.mkv"))           // in progress
	writeFile(t, filepath.Join(inc, "Stalled.Movie", "x.part"))   // orphan
	writeFile(t, filepath.Join(root, "shows", "Done", "e01.mkv")) // completed

	_, out, err := s.listMedia(context.Background(), nil, listMediaInput{Recursive: true})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"Foo":           filepath.Join(inc, "Foo"),
		"Stalled.Movie": filepath.Join(inc, "Stalled.Movie"),
		"Done":          filepath.Join(root, "shows", "Done"),
	}
	seen := 0
	for _, it := range out.Items {
		w, ok := want[it.Name]
		if !ok {
			continue
		}
		seen++
		if it.Path != w {
			t.Errorf("%s: path %q, want %q", it.Name, it.Path, w)
		}
		if _, err := os.Stat(it.Path); err != nil {
			t.Errorf("%s: reported path does not exist: %v", it.Name, err)
		}
		if len(it.Files) != 1 {
			t.Errorf("%s: recursive listing found %d files, want 1", it.Name, len(it.Files))
		}
	}
	if seen != len(want) {
		t.Fatalf("saw %d of %d items: %+v", seen, len(want), out.Items)
	}
}

// When an item has both a completed copy and a leftover in the incomplete
// directory, the completed copy is reported.
func TestListMediaPrefersCompletedCopy(t *testing.T) {
	s, root := newListServer(t)
	writeFile(t, filepath.Join(root, "movies", "Bar", "movie.mkv"))
	writeFile(t, filepath.Join(root, "incomplete", "Bar", "leftover.part"))

	_, out, _ := s.listMedia(context.Background(), nil, listMediaInput{Category: "movies"})
	if len(out.Items) != 1 || out.Items[0].Path != filepath.Join(root, "movies", "Bar") {
		t.Fatalf("items = %+v", out.Items)
	}
}
