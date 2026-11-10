package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// F94: with the incomplete directory outside the download path (as
// Transmission keeps it by default), the paths list_media reports there are
// usable by read_tags and move_media; paths elsewhere stay refused.
func TestIncompleteDirOutsideDownloadPath(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root, inc := filepath.Join(base, "downloads"), filepath.Join(base, "incomplete")
	for _, d := range []string{filepath.Join(root, "audiobooks"), filepath.Join(inc, "Orphan"), filepath.Join(base, "other")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mp3 := filepath.Join(inc, "Orphan", "a.mp3")
	rawMP3(t, mp3, 3, 0, rawFrame(3, "TIT2", "Title", 0))
	s := &server{config: Config{DownloadPath: root, IncompletePath: inc}}

	res, out, _ := s.readTags(context.Background(), nil, readTagsInput{Path: mp3})
	if res.IsError || len(out.Files) != 1 {
		t.Fatalf("read_tags on the incomplete dir: %v", res.Content)
	}
	res, _, _ = s.moveMedia(context.Background(), nil, moveMediaInput{
		Source: filepath.Join(inc, "Orphan"), Destination: "audiobooks/Orphan"})
	if res.IsError {
		t.Fatalf("move_media out of the incomplete dir: %v", res.Content)
	}
	if _, err := os.Stat(filepath.Join(root, "audiobooks", "Orphan", "a.mp3")); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(base, "other", "x.mp3")
	rawMP3(t, outside, 3, 0, rawFrame(3, "TIT2", "Title", 0))
	if res, _, _ := s.readTags(context.Background(), nil, readTagsInput{Path: outside}); !res.IsError {
		t.Fatal("read_tags accepted a path outside both roots")
	}
	if res, _, _ := s.readTags(context.Background(), nil, readTagsInput{Path: filepath.Join(inc, "..", "other", "x.mp3")}); !res.IsError {
		t.Fatal("read_tags accepted traversal out of the incomplete dir")
	}
}
