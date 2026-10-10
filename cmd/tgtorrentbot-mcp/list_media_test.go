package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	mcplib "github.com/minya/tgtorrentbot/internal/media"
)

func newListServer(t *testing.T) (*server, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &server{
		config: Config{DownloadPath: root},
		scan:   &mcplib.FilesystemScanner{DownloadPath: root, IncompletePath: filepath.Join(root, "incomplete")},
		jf:     mcplib.NewJellyfinClient("", ""),
		abs:    mcplib.NewAudiobookshelfClient("", ""),
	}, root
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestListMediaMtimeAndRecursive(t *testing.T) {
	s, root := newListServer(t)
	show := filepath.Join(root, "shows", "The Wire (2002)")
	writeFile(t, filepath.Join(show, "Season 01", "S01E01.mkv"))
	writeFile(t, filepath.Join(show, "Season 01", "S01E02.mkv"))

	_, out, _ := s.listMedia(context.Background(), nil, listMediaInput{Category: "shows"})
	if len(out.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(out.Items))
	}
	if out.Items[0].ModTime == "" {
		t.Error("expected mtime on filesystem item")
	}
	if out.Items[0].Files != nil {
		t.Error("files must be omitted unless recursive=true")
	}

	_, out, _ = s.listMedia(context.Background(), nil, listMediaInput{Category: "shows", Recursive: true})
	files := out.Items[0].Files
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %+v", files)
	}
	want := filepath.Join(show, "Season 01", "S01E01.mkv")
	if files[0].Path != want {
		t.Errorf("expected %q, got %q", want, files[0].Path)
	}
	if files[0].ModTime == "" {
		t.Error("expected file mtime")
	}
	if out.FilesTruncated {
		t.Error("unexpected truncation")
	}
}

func TestListFilesBudget(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"a", "b", "c"} {
		writeFile(t, filepath.Join(root, n))
	}
	budget := 2
	files, truncated := listFiles(root, &budget)
	if len(files) != 2 || !truncated {
		t.Fatalf("expected 2 files and truncation, got %d files, truncated=%v", len(files), truncated)
	}
}
