package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolvePath(t *testing.T) {
	// EvalSymlinks the temp dir so root comparisons work on macOS where
	// /var → /private/var.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Materialize directories used by the happy-path cases.
	for _, d := range []string{"audiobooks/Book", "music/Artist"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name    string
		in      string
		wantErr bool
		wantRel string
	}{
		{"relative under root", "audiobooks/Book", false, "audiobooks/Book"},
		{"absolute under root", filepath.Join(root, "music/Artist"), false, "music/Artist"},
		{"root itself", root, false, ""},
		{"empty", "", true, ""},
		{"traversal", "../../etc/passwd", true, ""},
		{"absolute outside root", "/tmp/somewhere-else-xyz", true, ""},
		{"nonexistent path", "no-such-dir", true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolvePath(root, tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			rel, _ := filepath.Rel(root, got)
			if rel == "." {
				rel = ""
			}
			if strings.ReplaceAll(rel, "\\", "/") != tt.wantRel {
				t.Errorf("resolvePath(%q) = %q (rel %q); want rel %q", tt.in, got, rel, tt.wantRel)
			}
		})
	}
}

func TestResolveDestPath(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "shows"), 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		in      string
		wantErr bool
		wantRel string
	}{
		{"non-existent under existing dir", "shows/The Wire (2002)", false, "shows/The Wire (2002)"},
		{"nested non-existent parents", "shows/The Wire (2002)/Season 01/ep.mkv", false, "shows/The Wire (2002)/Season 01/ep.mkv"},
		{"empty", "", true, ""},
		{"traversal", "../escape", true, ""},
		{"absolute outside root", "/tmp/escape-xyz", true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveDestPath(root, tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			rel, _ := filepath.Rel(root, got)
			if strings.ReplaceAll(rel, "\\", "/") != tt.wantRel {
				t.Errorf("resolveDestPath(%q) rel = %q; want %q", tt.in, rel, tt.wantRel)
			}
		})
	}
}

func TestResolveDestPathSymlinkParentEscape(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	outside, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// A symlinked dir inside root pointing outside: a destination beneath it
	// must be rejected even though the leaf doesn't exist yet.
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, err := resolveDestPath(root, "link/new-file"); err == nil {
		t.Error("expected error for destination under escaping symlink")
	}
}

func TestResolvePathSymlinks(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	// A file outside the download root, to be linked into it.
	outsideDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	outsideFile := filepath.Join(outsideDir, "secret.mp3")
	if err := os.WriteFile(outsideFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Symlink inside root pointing outside must be rejected.
	escapeLink := filepath.Join(root, "escape.mp3")
	if err := os.Symlink(outsideFile, escapeLink); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, err := resolvePath(root, "escape.mp3"); err == nil {
		t.Error("resolvePath on escaping symlink should have errored")
	}

	// Symlink inside root pointing inside root must resolve to the real path.
	realDir := filepath.Join(root, "album")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	realFile := filepath.Join(realDir, "track.mp3")
	if err := os.WriteFile(realFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	innerLink := filepath.Join(root, "alias.mp3")
	if err := os.Symlink(realFile, innerLink); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	got, err := resolvePath(root, "alias.mp3")
	if err != nil {
		t.Fatalf("resolvePath on inside symlink: %v", err)
	}
	if got != realFile {
		t.Errorf("resolvePath returned %q, want real path %q", got, realFile)
	}

	// Download root itself may be a symlink; paths beneath must still resolve.
	parent := t.TempDir()
	rootLink := filepath.Join(parent, "root-link")
	if err := os.Symlink(root, rootLink); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	got, err = resolvePath(rootLink, "album/track.mp3")
	if err != nil {
		t.Fatalf("resolvePath with symlinked root: %v", err)
	}
	if got != realFile {
		t.Errorf("resolvePath with symlinked root returned %q, want %q", got, realFile)
	}
}
