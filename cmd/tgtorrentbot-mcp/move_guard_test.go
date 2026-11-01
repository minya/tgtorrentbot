package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/odwrtw/transmission"
)

// F26: the root, category dirs and the incomplete dir cannot be moved, nor
// be the destination, and the torrent is not touched when refused.
func TestMoveMediaRefusesStructuralDirs(t *testing.T) {
	for _, tc := range []struct{ name, src, dst string }{
		{"category as source", "movies", "others/movies-old"},
		{"root as source", ".", "others/everything"},
		{"incomplete as source", "incomplete", "others/inc"},
		{"category as destination", "others/Item", "shows"},
		{"incomplete as destination", "others/Item", "incomplete"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, root := newMoveServer(t)
			for _, d := range []string{"movies/A", "incomplete/B", "others/Item"} {
				if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if tc.dst == "shows" || tc.dst == "incomplete" {
				os.RemoveAll(filepath.Join(root, tc.dst)) // destination must not exist
			}
			fake := &fakeTorrents{torrents: []*transmission.Torrent{{ID: 1}}}
			s.trans = fake

			res, out, _ := s.moveMedia(context.Background(), nil, moveMediaInput{Source: tc.src, Destination: tc.dst, TorrentID: intPtr(1)})

			if !res.IsError || out.Moved {
				t.Fatalf("move %s -> %s was allowed: %+v", tc.src, tc.dst, out)
			}
			if len(fake.removed) != 0 {
				t.Fatal("torrent was removed although the move was refused")
			}
			if _, err := os.Stat(filepath.Join(root, "movies", "A")); err != nil {
				t.Fatal("movies/A is gone")
			}
		})
	}
}

// F27: a symlink source is refused rather than moving its target.
func TestMoveMediaRefusesSymlinkSource(t *testing.T) {
	s, root := newMoveServer(t)
	real := filepath.Join(root, "movies", "Real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "shows"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "shows", "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	res, out, _ := s.moveMedia(context.Background(), nil, moveMediaInput{Source: "shows/link", Destination: "shows/Renamed"})

	if !res.IsError || out.Moved {
		t.Fatalf("symlink source was moved: %+v", out)
	}
	if _, err := os.Stat(real); err != nil {
		t.Fatal("the link's target was moved")
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the link changed")
	}
}

// Items inside a category directory still move normally.
func TestMoveMediaStillMovesItems(t *testing.T) {
	s, root := newMoveServer(t)
	if err := os.MkdirAll(filepath.Join(root, "incomplete", "Leftover"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, out, _ := s.moveMedia(context.Background(), nil, moveMediaInput{Source: "incomplete/Leftover", Destination: "others/Leftover"})
	if res.IsError || !out.Moved {
		t.Fatalf("normal move refused: %+v", res.Content)
	}
}
