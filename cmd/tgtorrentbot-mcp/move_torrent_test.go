package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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

// showTorrent is torrent 7, whose data is the setupShow source.
func showTorrent(root string) *transmission.Torrent {
	return &transmission.Torrent{ID: 7, Name: "Proslushka.S01.2002", DownloadDir: filepath.Join(root, "shows"), PercentDone: 1}
}

func TestMoveMediaRemovesTorrentFirst(t *testing.T) {
	s, root, src := setupShow(t)
	fake := &fakeTorrents{torrents: []*transmission.Torrent{showTorrent(root), {ID: 8}}, src: src}
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
	s, root, src := setupShow(t)
	s.trans = &fakeTorrents{
		torrents:  []*transmission.Torrent{showTorrent(root)},
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
	fake := &fakeTorrents{torrents: []*transmission.Torrent{showTorrent(root)}, src: src}
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

// F28: a torrent_id that belongs to another item is refused before anything
// is removed or moved.
func TestMoveMediaRefusesTorrentThatDoesNotOwnSource(t *testing.T) {
	s, root, src := setupShow(t)
	other := &transmission.Torrent{ID: 9, Name: "Some Movie (2020)", DownloadDir: filepath.Join(root, "movies")}
	fake := &fakeTorrents{torrents: []*transmission.Torrent{showTorrent(root), other}, src: src}
	s.trans = fake

	res, out, _ := s.moveMedia(context.Background(), nil, moveMediaInput{
		Source:      "shows/Proslushka.S01.2002",
		Destination: "shows/The Wire (2002)",
		TorrentID:   intPtr(9),
	})
	if !res.IsError {
		t.Fatalf("expected an error for a torrent that does not own the source, got %+v", out)
	}
	if len(fake.removed) != 0 {
		t.Fatalf("no torrent may be removed, got %v", fake.removed)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("source must be untouched: %v", err)
	}
}

// An in-progress torrent's data is in the incomplete dir under its name, and
// moving one file out of a torrent's folder also takes data from it.
func TestMoveMediaTorrentOwnershipVariants(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		mk           string
	}{
		{"incomplete", "incomplete/Proslushka.S01.2002", "incomplete/Proslushka.S01.2002"},
		{"file inside", "shows/Proslushka.S01.2002/e01.mkv", "shows/Proslushka.S01.2002/e01.mkv"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, root := newMoveServer(t)
			p := filepath.Join(root, tc.mk)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, nil, 0o644); err != nil {
				t.Fatal(err)
			}
			fake := &fakeTorrents{torrents: []*transmission.Torrent{showTorrent(root)}, src: p}
			s.trans = fake
			res, _, _ := s.moveMedia(context.Background(), nil, moveMediaInput{
				Source: tc.source, Destination: "shows/Moved", TorrentID: intPtr(7),
			})
			if res.IsError {
				t.Fatalf("unexpected error: %v", res.Content)
			}
			if len(fake.removed) != 1 {
				t.Fatalf("torrent 7 must be removed, got %v", fake.removed)
			}
		})
	}
}

// F91: data a torrent still tracks is not moved unless that torrent is named.
func TestMoveMediaRefusesTrackedDataWithoutTorrentID(t *testing.T) {
	s, root, src := setupShow(t)
	fake := &fakeTorrents{torrents: []*transmission.Torrent{showTorrent(root)}, src: src}
	s.trans = fake

	res, out, _ := s.moveMedia(context.Background(), nil, moveMediaInput{
		Source: "shows/Proslushka.S01.2002", Destination: "shows/The Wire (2002)",
	})
	if !res.IsError || out.Moved {
		t.Fatalf("moved data torrent 7 still tracks: %+v", out)
	}
	if text := res.Content[0].(*mcp.TextContent).Text; !strings.Contains(text, "7") {
		t.Errorf("error %q does not name the owning torrent", text)
	}
	if len(fake.removed) != 0 {
		t.Fatalf("removed %v, want nothing", fake.removed)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("source must be untouched: %v", err)
	}
}

// F91: when two torrents track the source, naming one is not enough.
func TestMoveMediaRefusesWhenAnotherTorrentAlsoOwnsSource(t *testing.T) {
	s, root, src := setupShow(t)
	second := &transmission.Torrent{ID: 9, Name: "Proslushka.S01.2002", DownloadDir: filepath.Join(root, "shows"), PercentDone: 1}
	fake := &fakeTorrents{torrents: []*transmission.Torrent{showTorrent(root), second}, src: src}
	s.trans = fake

	res, out, _ := s.moveMedia(context.Background(), nil, moveMediaInput{
		Source: "shows/Proslushka.S01.2002", Destination: "shows/The Wire (2002)", TorrentID: intPtr(7),
	})
	if !res.IsError || out.Moved || len(fake.removed) != 0 {
		t.Fatalf("moved with torrent 9 still tracking the source: %+v, removed %v", out, fake.removed)
	}
	if text := res.Content[0].(*mcp.TextContent).Text; !strings.Contains(text, "9") {
		t.Errorf("error %q does not name torrent 9", text)
	}
}

type unreachableTorrents struct{}

func (unreachableTorrents) GetTorrents() ([]*transmission.Torrent, error) {
	return nil, errors.New("connection refused")
}
func (unreachableTorrents) RemoveTorrents([]*transmission.Torrent, bool) error { return nil }

// With Transmission down and no torrent named, the move still happens, with
// a note that it was not checked.
func TestMoveMediaUncheckedWhenTransmissionDown(t *testing.T) {
	s, _, _ := setupShow(t)
	s.trans = unreachableTorrents{}
	res, out, _ := s.moveMedia(context.Background(), nil, moveMediaInput{
		Source: "shows/Proslushka.S01.2002", Destination: "shows/The Wire (2002)",
	})
	if res.IsError || !out.Moved || !strings.Contains(out.TorrentNote, "without checking") {
		t.Fatalf("result %+v, want a move with a note", out)
	}
}

// F111: a download still in progress is not untracked and moved.
func TestMoveMediaRefusesDownloadInProgress(t *testing.T) {
	s, root, src := setupShow(t)
	downloading := showTorrent(root)
	downloading.PercentDone = 0.4
	fake := &fakeTorrents{torrents: []*transmission.Torrent{downloading}, src: src}
	s.trans = fake

	res, out, _ := s.moveMedia(context.Background(), nil, moveMediaInput{
		Source: "shows/Proslushka.S01.2002", Destination: "shows/The Wire (2002)", TorrentID: intPtr(7),
	})
	if !res.IsError || out.Moved || len(fake.removed) != 0 {
		t.Fatalf("an in-progress download was untracked or moved: %+v, removed %v", out, fake.removed)
	}
	if text := res.Content[0].(*mcp.TextContent).Text; !strings.Contains(text, "still downloading") {
		t.Errorf("error %q does not say it is still downloading", text)
	}
}
