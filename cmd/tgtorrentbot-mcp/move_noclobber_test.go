package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/odwrtw/transmission"
)

// racingTorrents creates the move destination while the torrent is being
// removed, i.e. after move_media's existence pre-check and before the rename,
// as a concurrent move_media or another writer would.
type racingTorrents struct {
	src    string // the source path, which torrent 1 owns
	create func() error
}

func (r *racingTorrents) GetTorrents() ([]*transmission.Torrent, error) {
	return []*transmission.Torrent{{ID: 1, Name: filepath.Base(r.src), DownloadDir: filepath.Dir(r.src)}}, nil
}

func (r *racingTorrents) RemoveTorrents([]*transmission.Torrent, bool) error {
	return r.create()
}

func TestMoveMediaDoesNotClobberFileAppearingMidMove(t *testing.T) {
	s, root := newMoveServer(t)
	src := filepath.Join(root, "shows", "a.mkv")
	dst := filepath.Join(root, "shows", "b.mkv")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("SOURCE"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.trans = &racingTorrents{src: src, create: func() error { return os.WriteFile(dst, []byte("PRECIOUS"), 0o644) }}

	res, out, _ := s.moveMedia(context.Background(), nil, moveMediaInput{
		Source: "shows/a.mkv", Destination: "shows/b.mkv", TorrentID: intPtr(1),
	})

	if !res.IsError || out.Moved {
		t.Fatalf("move succeeded over a destination that appeared mid-move: %+v", out)
	}
	if data, _ := os.ReadFile(dst); string(data) != "PRECIOUS" {
		t.Fatalf("destination was clobbered: %q", data)
	}
	if data, _ := os.ReadFile(src); string(data) != "SOURCE" {
		t.Fatalf("source changed: %q", data)
	}
}

func TestMoveMediaDoesNotReplaceEmptyDirAppearingMidMove(t *testing.T) {
	s, root := newMoveServer(t)
	src := filepath.Join(root, "shows", "Old.Name")
	dst := filepath.Join(root, "shows", "New Name (2002)")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	s.trans = &racingTorrents{src: src, create: func() error { return os.Mkdir(dst, 0o755) }}

	res, out, _ := s.moveMedia(context.Background(), nil, moveMediaInput{
		Source: "shows/Old.Name", Destination: "shows/New Name (2002)", TorrentID: intPtr(1),
	})

	if !res.IsError || out.Moved {
		t.Fatalf("move replaced an empty directory that appeared mid-move: %+v", out)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("source is gone: %v", err)
	}
}

func TestRenameNoReplace(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	os.WriteFile(a, []byte("a"), 0o644)
	if err := renameNoReplace(a, b); err != nil {
		t.Fatalf("rename to a free name: %v", err)
	}
	os.WriteFile(a, []byte("a2"), 0o644)
	if err := renameNoReplace(a, b); err == nil {
		t.Fatal("rename over an existing file succeeded")
	}
	if data, _ := os.ReadFile(b); string(data) != "a" {
		t.Fatalf("existing file was replaced: %q", data)
	}
}
