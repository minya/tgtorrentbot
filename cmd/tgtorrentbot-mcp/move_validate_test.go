package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/odwrtw/transmission"
)

// F29: moving a directory into itself is refused before the torrent is
// untracked, and leaves no stray directory inside the source.
func TestMoveMediaIntoSelfKeepsTorrent(t *testing.T) {
	s, root, src := setupShow(t)
	fake := &fakeTorrents{torrents: []*transmission.Torrent{showTorrent(root)}, src: src}
	s.trans = fake

	res, out, _ := s.moveMedia(context.Background(), nil, moveMediaInput{
		Source:      "shows/Proslushka.S01.2002",
		Destination: "shows/Proslushka.S01.2002/Season 1/X",
		TorrentID:   intPtr(7),
	})
	if !res.IsError || out.Moved {
		t.Fatalf("move into itself succeeded: %+v", out)
	}
	if len(fake.removed) != 0 {
		t.Fatalf("torrent removed for a move that cannot happen: %v", fake.removed)
	}
	if _, err := os.Stat(filepath.Join(src, "Season 1")); !os.IsNotExist(err) {
		t.Fatalf("stray directory left inside the source: %v", err)
	}
}

func TestMkdirAllTrackedAndRemoveCreated(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "keep"), 0o755); err != nil {
		t.Fatal(err)
	}
	created, err := mkdirAllTracked(filepath.Join(root, "keep", "a", "b"))
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 2 || created[0] != filepath.Join(root, "keep", "a", "b") {
		t.Fatalf("created = %v, want [keep/a/b keep/a]", created)
	}
	removeCreated(created)
	if _, err := os.Stat(filepath.Join(root, "keep", "a")); !os.IsNotExist(err) {
		t.Fatalf("created dir not removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "keep")); err != nil {
		t.Fatalf("pre-existing dir removed: %v", err)
	}
}

func TestSameDeviceWithinTempDir(t *testing.T) {
	root := t.TempDir()
	same, ok := sameDevice(root, existingAncestor(filepath.Join(root, "x", "y")))
	if ok && !same {
		t.Fatal("two paths in one temp dir reported on different devices")
	}
}
