package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func newMoveServer(t *testing.T) (*server, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &server{config: Config{DownloadPath: root}}, root
}

func TestMoveMediaRenameDir(t *testing.T) {
	s, root := newMoveServer(t)
	srcDir := filepath.Join(root, "shows", "Proslushka.S01.2002")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "ep.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, out, _ := s.moveMedia(context.Background(), nil, moveMediaInput{
		Source:      "shows/Proslushka.S01.2002",
		Destination: "shows/The Wire (2002)",
	})
	if res.IsError {
		t.Fatalf("unexpected error: %v", res.Content)
	}
	if !out.Moved {
		t.Fatal("expected Moved=true")
	}
	if _, err := os.Stat(filepath.Join(root, "shows", "The Wire (2002)", "ep.mkv")); err != nil {
		t.Fatalf("renamed file missing: %v", err)
	}
	if _, err := os.Stat(srcDir); !os.IsNotExist(err) {
		t.Fatalf("source should be gone, got err=%v", err)
	}
}

func TestMoveMediaAutoCreatesParent(t *testing.T) {
	s, root := newMoveServer(t)
	showDir := filepath.Join(root, "shows", "The Wire (2002)")
	if err := os.MkdirAll(showDir, 0o755); err != nil {
		t.Fatal(err)
	}
	epFile := filepath.Join(showDir, "ep01.mkv")
	if err := os.WriteFile(epFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Destination's "Season 01" parent does not exist yet.
	res, _, _ := s.moveMedia(context.Background(), nil, moveMediaInput{
		Source:      "shows/The Wire (2002)/ep01.mkv",
		Destination: "shows/The Wire (2002)/Season 01/ep01.mkv",
	})
	if res.IsError {
		t.Fatalf("unexpected error: %v", res.Content)
	}
	if _, err := os.Stat(filepath.Join(showDir, "Season 01", "ep01.mkv")); err != nil {
		t.Fatalf("file not moved into auto-created dir: %v", err)
	}
}

func TestMoveMediaRefusesClobber(t *testing.T) {
	s, root := newMoveServer(t)
	for _, d := range []string{"shows/A", "shows/B"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	res, _, _ := s.moveMedia(context.Background(), nil, moveMediaInput{
		Source:      "shows/A",
		Destination: "shows/B",
	})
	if !res.IsError {
		t.Fatal("expected error when destination exists")
	}
	if _, err := os.Stat(filepath.Join(root, "shows", "A")); err != nil {
		t.Fatalf("source should be untouched after refused clobber: %v", err)
	}
}

func TestMoveMediaRejectsEscape(t *testing.T) {
	s, root := newMoveServer(t)
	srcDir := filepath.Join(root, "shows", "X")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cases := []moveMediaInput{
		{Source: "shows/X", Destination: "../escape"},
		{Source: "shows/X", Destination: "/tmp/escape-xyz"},
		{Source: "../../etc/passwd", Destination: "shows/Y"},
	}
	for _, in := range cases {
		res, _, _ := s.moveMedia(context.Background(), nil, in)
		if !res.IsError {
			t.Errorf("expected error for %+v", in)
		}
	}
	if _, err := os.Stat(srcDir); err != nil {
		t.Fatalf("source should be untouched: %v", err)
	}
}
