package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// caseInsensitive reports whether dir's filesystem ignores case.
func caseInsensitive(t *testing.T, dir string) bool {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "probe"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := os.Stat(filepath.Join(dir, "PROBE"))
	os.Remove(filepath.Join(dir, "probe"))
	return err == nil
}

// F92: on a case-insensitive filesystem a differently-cased spelling of a
// category directory, or of the source itself, does not get past the guards.
func TestMoveMediaGuardsIgnoreCase(t *testing.T) {
	s, root := newMoveServer(t)
	if !caseInsensitive(t, root) {
		t.Skip("filesystem is case-sensitive")
	}
	if err := os.MkdirAll(filepath.Join(root, "shows", "Show"), 0o755); err != nil {
		t.Fatal(err)
	}

	// The category directory spelled "Shows".
	res, out, _ := s.moveMedia(context.Background(), nil, moveMediaInput{Source: "Shows", Destination: "others/everything"})
	if !res.IsError || out.Moved {
		t.Fatalf("moved the shows category directory as %q: %+v", "Shows", out)
	}
	// A destination inside the source, spelled differently.
	res, out, _ = s.moveMedia(context.Background(), nil, moveMediaInput{Source: "shows/Show", Destination: "shows/SHOW/Season 1"})
	if !res.IsError || out.Moved {
		t.Fatalf("moved a directory into itself: %+v", out)
	}
	if _, err := os.Stat(filepath.Join(root, "shows", "Show")); err != nil {
		t.Fatalf("source changed: %v", err)
	}
}
