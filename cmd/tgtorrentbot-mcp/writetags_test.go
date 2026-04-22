package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/bogem/id3v2/v2"
)

// TestWriteTagsDiscTrack constructs a tiny mp3 with ID3v2 frames, calls the
// MCP tool handler end-to-end with disc+track overrides, and verifies the
// file ends up with those frames set. Proves the "unknown tag key" rejection
// reported from the Pi isn't a bug in this code.
func TestWriteTagsDiscTrack(t *testing.T) {
	root := t.TempDir()
	mp3 := filepath.Join(root, "track.mp3")
	if err := seedEmptyMP3(t, mp3); err != nil {
		t.Fatalf("seed: %v", err)
	}

	srv := &server{config: Config{DownloadPath: root}}

	in := writeTagsInput{
		Path: "track.mp3",
		Tags: map[string]string{
			"disc":  "1/2",
			"track": "01/13",
			"album": "Лучшие Песни",
		},
	}
	res, out, err := srv.writeTags(context.Background(), nil, in)
	if err != nil {
		t.Fatalf("writeTags err: %v", err)
	}
	if res.IsError {
		t.Fatalf("writeTags returned IsError=true: %+v", res.Content)
	}
	if len(out.Files) != 1 {
		t.Fatalf("want 1 file, got %d", len(out.Files))
	}
	if out.Files[0].Error != "" {
		t.Fatalf("file error: %s", out.Files[0].Error)
	}
	if !out.Files[0].Changed {
		t.Fatalf("expected Changed=true")
	}

	tag, err := id3v2.Open(mp3, id3v2.Options{Parse: true})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer tag.Close()
	if got := tag.GetTextFrame(tag.CommonID("Part of a set")).Text; got != "1/2" {
		t.Errorf("disc (TPOS) = %q, want %q", got, "1/2")
	}
	if got := tag.GetTextFrame(tag.CommonID("Track number/Position in set")).Text; got != "01/13" {
		t.Errorf("track (TRCK) = %q, want %q", got, "01/13")
	}
	if got := tag.Album(); got != "Лучшие Песни" {
		t.Errorf("album = %q, want %q", got, "Лучшие Песни")
	}
}

// seedEmptyMP3 writes a minimal file that id3v2 can open (empty ID3v2 header
// followed by a tiny "silence" payload). id3v2.Open requires a readable file;
// it doesn't validate the audio frames.
func seedEmptyMP3(t *testing.T, path string) error {
	t.Helper()
	tag := id3v2.NewEmptyTag()
	tag.SetDefaultEncoding(id3v2.EncodingUTF8)
	tag.SetTitle("placeholder")
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if _, err := tag.WriteTo(f); err != nil {
		f.Close()
		return err
	}
	// id3v2.Save expects music bytes after the tag; a few zero bytes satisfy
	// the copy loop in Save without needing a real MPEG frame.
	if _, err := f.Write(make([]byte, 1024)); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
