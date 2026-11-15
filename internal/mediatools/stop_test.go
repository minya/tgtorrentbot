package mediatools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bogem/id3v2/v2"
	"github.com/odwrtw/transmission"
)

// stopAfter is a context whose Err turns non-nil after n calls, to stop a
// tool at a chosen safe point.
type stopAfter struct {
	context.Context
	n int
}

func (c *stopAfter) Err() error {
	if c.n <= 0 {
		return context.Canceled
	}
	c.n--
	return nil
}

// write_tags stops between files when asked and reports how far it
// got; every file it touched is saved whole, the rest are untouched.
func TestWriteTagsStopsBetweenFiles(t *testing.T) {
	root := t.TempDir()
	for i := 1; i <= 5; i++ {
		seedMP3(t, filepath.Join(root, "book", fmt.Sprintf("%d.mp3", i)), 0o644, func(tag *id3v2.Tag) { tag.SetAlbum("old") })
	}
	srv := &Tools{config: Config{DownloadPath: root}}
	// One check before untracking, then one per file: stop before the 3rd.
	ctx := &stopAfter{Context: context.Background(), n: 3}
	res, out, err := srv.writeTags(ctx, nil, writeTagsInput{Path: "book", Recursive: true, Tags: map[string]string{"album": "new"}})
	if err != nil || res.IsError {
		t.Fatalf("err %v, result %+v", err, res)
	}
	if !out.Stopped || len(out.Files) != 2 || out.Changed != 2 || out.Total != 5 || !strings.HasPrefix(out.Summary, "stopped after 2 of 5 files") {
		t.Fatalf("out %+v", out)
	}
	for i := 1; i <= 5; i++ {
		tag, err := id3v2.Open(filepath.Join(root, "book", fmt.Sprintf("%d.mp3", i)), id3v2.Options{Parse: true})
		if err != nil {
			t.Fatal(err)
		}
		want := "old"
		if i <= 2 {
			want = "new"
		}
		if got := tag.Album(); got != want {
			t.Errorf("%d.mp3 album %q, want %q", i, got, want)
		}
		tag.Close()
	}
}

// A stop requested through SetStop before the call changes nothing,
// and the torrent is not untracked.
func TestMutatingToolsStopBeforeUntracking(t *testing.T) {
	root := t.TempDir()
	seedMP3(t, filepath.Join(root, "shows", "X", "1.mp3"), 0o644, func(tag *id3v2.Tag) { tag.SetAlbum("old") })
	trans := &recordingTorrents{}
	srv := &Tools{config: Config{DownloadPath: root}, trans: trans}
	stop, cancel := context.WithCancel(context.Background())
	cancel()
	srv.SetStop(stop)
	id := 7

	res, _, _ := srv.writeTags(context.Background(), nil, writeTagsInput{Path: "shows/X", Recursive: true, Tags: map[string]string{"album": "new"}, TorrentID: &id})
	if !res.IsError || !strings.Contains(fmt.Sprint(res.Content[0]), "no file was changed") {
		t.Fatalf("write_tags: %+v", res.Content)
	}
	res, _, _ = srv.moveMedia(context.Background(), nil, moveMediaInput{Source: "shows/X", Destination: "shows/Y", TorrentID: &id})
	if !res.IsError || !strings.Contains(fmt.Sprint(res.Content[0]), "nothing was moved") {
		t.Fatalf("move_media: %+v", res.Content)
	}
	if trans.removeCalls != 0 {
		t.Fatal("torrent untracked although the call stopped")
	}
	if _, err := os.Stat(filepath.Join(root, "shows", "X", "1.mp3")); err != nil {
		t.Fatal("source moved")
	}
}

// recordingTorrents counts removals; it tracks nothing.
type recordingTorrents struct{ removeCalls int }

func (r *recordingTorrents) GetTorrents() ([]*transmission.Torrent, error) { return nil, nil }
func (r *recordingTorrents) RemoveTorrents([]*transmission.Torrent, bool) error {
	r.removeCalls++
	return nil
}
