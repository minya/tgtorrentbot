package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/bogem/id3v2/v2"
)

// F72: with TGT_DOWNLOADPATH behind a symlink, the paths the tools return
// are relative to the root and are accepted when passed back.
func TestSymlinkedDownloadRootRoundTrips(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "downloads")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	seedMP3(t, filepath.Join(real, "audiobooks", "Book", "01.mp3"), 0o644, func(tag *id3v2.Tag) { tag.SetTitle("one") })

	t.Setenv("TGT_DOWNLOADPATH", link)
	t.Setenv("TGT_INCOMPLETE_PATH", "")
	t.Setenv("TGT_MCP_TOKEN", "")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	srv := &server{config: cfg}

	_, out, err := srv.readTags(context.Background(), nil, readTagsInput{Path: "audiobooks/Book", Recursive: true})
	if err != nil || len(out.Files) != 1 {
		t.Fatalf("read_tags = %+v, %v", out, err)
	}
	got := out.Files[0].Path
	if filepath.IsAbs(got) {
		t.Fatalf("read_tags returned absolute path %q, want one relative to the root", got)
	}
	res, again, _ := srv.readTags(context.Background(), nil, readTagsInput{Path: got})
	if res.IsError || len(again.Files) != 1 || again.Files[0].Title != "one" {
		t.Fatalf("passing %q back to read_tags: %+v %+v", got, res.Content, again)
	}
}
