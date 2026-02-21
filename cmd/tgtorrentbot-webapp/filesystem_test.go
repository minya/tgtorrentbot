package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanCategory(t *testing.T) {
	tmp := t.TempDir()
	// Create structure: {tmp}/movies/MovieA/ and {tmp}/movies/MovieB/
	moviesDir := filepath.Join(tmp, "movies")
	os.MkdirAll(filepath.Join(moviesDir, "MovieA"), 0o755)
	os.MkdirAll(filepath.Join(moviesDir, "MovieB"), 0o755)
	// Add a file inside MovieA
	os.WriteFile(filepath.Join(moviesDir, "MovieA", "video.mkv"), make([]byte, 1024), 0o644)
	// Add a plain file at the category root (should be ignored)
	os.WriteFile(filepath.Join(moviesDir, "readme.txt"), []byte("hi"), 0o644)

	scanner := &filesystemScanner{downloadPath: tmp}
	items, err := scanner.ScanCategory("movies")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	// Find MovieA and check size
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
}

func TestScanCategoryMissing(t *testing.T) {
	tmp := t.TempDir()
	scanner := &filesystemScanner{downloadPath: tmp}
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

	scanner := &filesystemScanner{incompletePath: incDir}
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

func TestScanIncompleteMissing(t *testing.T) {
	scanner := &filesystemScanner{incompletePath: "/nonexistent/path/xyz"}
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

	scanner := &filesystemScanner{downloadPath: tmp}
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
