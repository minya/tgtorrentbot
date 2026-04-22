package media

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanCategory(t *testing.T) {
	tmp := t.TempDir()
	moviesDir := filepath.Join(tmp, "movies")
	os.MkdirAll(filepath.Join(moviesDir, "MovieA"), 0o755)
	os.MkdirAll(filepath.Join(moviesDir, "MovieB"), 0o755)
	os.WriteFile(filepath.Join(moviesDir, "MovieA", "video.mkv"), make([]byte, 1024), 0o644)
	os.WriteFile(filepath.Join(moviesDir, "standalone.mkv"), make([]byte, 2048), 0o644)

	scanner := &FilesystemScanner{DownloadPath: tmp}
	items, err := scanner.ScanCategory("movies")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items (2 dirs + 1 file), got %d", len(items))
	}

	var movieA *FsItem
	for i := range items {
		if items[i].Name == "MovieA" {
			movieA = &items[i]
		}
	}
	if movieA == nil {
		t.Fatal("MovieA not found")
	}
	if movieA.Size != 1024 {
		t.Errorf("expected size 1024, got %d", movieA.Size)
	}
	if movieA.IsIncomplete {
		t.Error("expected IsIncomplete=false")
	}

	var standalone *FsItem
	for i := range items {
		if items[i].Name == "standalone.mkv" {
			standalone = &items[i]
		}
	}
	if standalone == nil {
		t.Fatal("standalone.mkv should be included")
	}
	if standalone.Size != 2048 {
		t.Errorf("expected size 2048, got %d", standalone.Size)
	}
}

func TestScanCategoryMissing(t *testing.T) {
	tmp := t.TempDir()
	scanner := &FilesystemScanner{DownloadPath: tmp}
	items, err := scanner.ScanCategory("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if items != nil {
		t.Fatalf("expected nil, got %v", items)
	}
}

func TestScanIncomplete(t *testing.T) {
	tmp := t.TempDir()
	incDir := filepath.Join(tmp, "incomplete")
	os.MkdirAll(filepath.Join(incDir, "PartialDownload"), 0o755)
	os.WriteFile(filepath.Join(incDir, "PartialDownload", "part.dat"), make([]byte, 512), 0o644)

	scanner := &FilesystemScanner{IncompletePath: incDir}
	items, err := scanner.ScanIncomplete()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Name != "PartialDownload" {
		t.Errorf("expected PartialDownload, got %s", items[0].Name)
	}
	if items[0].Size != 512 {
		t.Errorf("expected size 512, got %d", items[0].Size)
	}
	if !items[0].IsIncomplete {
		t.Error("expected IsIncomplete=true")
	}
}

func TestScanIncompleteStandaloneFile(t *testing.T) {
	tmp := t.TempDir()
	incDir := filepath.Join(tmp, "incomplete")
	os.MkdirAll(incDir, 0o755)
	os.WriteFile(filepath.Join(incDir, "movie.mkv"), make([]byte, 4096), 0o644)

	scanner := &FilesystemScanner{IncompletePath: incDir}
	items, err := scanner.ScanIncomplete()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Name != "movie.mkv" {
		t.Errorf("expected movie.mkv, got %s", items[0].Name)
	}
	if items[0].Size != 4096 {
		t.Errorf("expected size 4096, got %d", items[0].Size)
	}
	if !items[0].IsIncomplete {
		t.Error("expected IsIncomplete=true")
	}
}

func TestScanIncompleteMissing(t *testing.T) {
	scanner := &FilesystemScanner{IncompletePath: "/nonexistent/path/xyz"}
	items, err := scanner.ScanIncomplete()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if items != nil {
		t.Fatalf("expected nil, got %v", items)
	}
}

func TestScanCategoryNestedFiles(t *testing.T) {
	tmp := t.TempDir()
	showDir := filepath.Join(tmp, "shows", "MyShow", "Season1")
	os.MkdirAll(showDir, 0o755)
	os.WriteFile(filepath.Join(showDir, "ep1.mkv"), make([]byte, 100), 0o644)
	os.WriteFile(filepath.Join(showDir, "ep2.mkv"), make([]byte, 200), 0o644)

	scanner := &FilesystemScanner{DownloadPath: tmp}
	items, err := scanner.ScanCategory("shows")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Name != "MyShow" {
		t.Errorf("expected MyShow, got %s", items[0].Name)
	}
	if items[0].Size != 300 {
		t.Errorf("expected size 300, got %d", items[0].Size)
	}
}
