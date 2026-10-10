package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/odwrtw/transmission"
)

type fakeTorrents struct {
	torrents  []*transmission.Torrent
	removeErr error
	removed   []int
	keptData  bool
	// srcAtRemove records whether the source still existed when the torrent
	// was removed, to verify removal happens before the move.
	src         string
	srcAtRemove bool
}

func (f *fakeTorrents) GetTorrents() ([]*transmission.Torrent, error) {
	return f.torrents, nil
}

func (f *fakeTorrents) RemoveTorrents(ts []*transmission.Torrent, removeData bool) error {
	if f.removeErr != nil {
		return f.removeErr
	}
	_, err := os.Stat(f.src)
	f.srcAtRemove = err == nil
	f.keptData = !removeData
	for _, t := range ts {
		f.removed = append(f.removed, t.ID)
	}
	return nil
}

func setupShow(t *testing.T) (*server, string, string) {
	t.Helper()
	s, root := newMoveServer(t)
	src := filepath.Join(root, "shows", "Proslushka.S01.2002")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	return s, root, src
}

func intPtr(i int) *int { return &i }

func TestMoveMediaRemovesTorrentFirst(t *testing.T) {
	s, root, src := setupShow(t)
	fake := &fakeTorrents{torrents: []*transmission.Torrent{{ID: 7}, {ID: 8}}, src: src}
	s.trans = fake

	res, out, _ := s.moveMedia(context.Background(), nil, moveMediaInput{
		Source:      "shows/Proslushka.S01.2002",
		Destination: "shows/The Wire (2002)",
		TorrentID:   intPtr(7),
	})
	if res.IsError {
		t.Fatalf("unexpected error: %v", res.Content)
	}
	if !out.Moved || !out.TorrentRemoved {
		t.Fatalf("expected moved and torrent removed, got %+v", out)
	}
	if len(fake.removed) != 1 || fake.removed[0] != 7 {
		t.Fatalf("expected only torrent 7 removed, got %v", fake.removed)
	}
	if !fake.keptData {
		t.Fatal("torrent data must be kept")
	}
	if !fake.srcAtRemove {
		t.Fatal("torrent must be removed before the files are moved")
	}
	if _, err := os.Stat(filepath.Join(root, "shows", "The Wire (2002)")); err != nil {
		t.Fatalf("destination missing: %v", err)
	}
}

func TestMoveMediaMissingTorrentStillMoves(t *testing.T) {
	s, _, src := setupShow(t)
	fake := &fakeTorrents{torrents: []*transmission.Torrent{{ID: 8}}, src: src}
	s.trans = fake

	res, out, _ := s.moveMedia(context.Background(), nil, moveMediaInput{
		Source:      "shows/Proslushka.S01.2002",
		Destination: "shows/The Wire (2002)",
		TorrentID:   intPtr(7),
	})
	if res.IsError {
		t.Fatalf("unexpected error: %v", res.Content)
	}
	if !out.Moved || out.TorrentRemoved || out.TorrentNote == "" {
		t.Fatalf("expected move with note and no removal, got %+v", out)
	}
	if len(fake.removed) != 0 {
		t.Fatalf("nothing should be removed, got %v", fake.removed)
	}
}

func TestMoveMediaRemoveFailureAbortsMove(t *testing.T) {
	s, _, src := setupShow(t)
	s.trans = &fakeTorrents{
		torrents:  []*transmission.Torrent{{ID: 7}},
		removeErr: errors.New("rpc down"),
		src:       src,
	}

	res, _, _ := s.moveMedia(context.Background(), nil, moveMediaInput{
		Source:      "shows/Proslushka.S01.2002",
		Destination: "shows/The Wire (2002)",
		TorrentID:   intPtr(7),
	})
	if !res.IsError {
		t.Fatal("expected error when torrent removal fails")
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("source must be untouched: %v", err)
	}
}

func TestMoveMediaTorrentIDWithoutTransmission(t *testing.T) {
	s, _, src := setupShow(t)
	res, _, _ := s.moveMedia(context.Background(), nil, moveMediaInput{
		Source:      "shows/Proslushka.S01.2002",
		Destination: "shows/The Wire (2002)",
		TorrentID:   intPtr(7),
	})
	if !res.IsError {
		t.Fatal("expected error when torrent_id is given but Transmission isn't configured")
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("source must be untouched: %v", err)
	}
}

func TestMoveMediaInvalidDestinationKeepsTorrent(t *testing.T) {
	s, root, src := setupShow(t)
	if err := os.MkdirAll(filepath.Join(root, "shows", "Taken"), 0o755); err != nil {
		t.Fatal(err)
	}
	fake := &fakeTorrents{torrents: []*transmission.Torrent{{ID: 7}}, src: src}
	s.trans = fake

	res, _, _ := s.moveMedia(context.Background(), nil, moveMediaInput{
		Source:      "shows/Proslushka.S01.2002",
		Destination: "shows/Taken",
		TorrentID:   intPtr(7),
	})
	if !res.IsError {
		t.Fatal("expected error for existing destination")
	}
	if len(fake.removed) != 0 {
		t.Fatalf("torrent must not be removed when the move is rejected, got %v", fake.removed)
	}
}
