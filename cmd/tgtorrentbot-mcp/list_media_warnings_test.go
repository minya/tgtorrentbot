package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minya/tgtorrentbot/internal/media"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/odwrtw/transmission"
)

type failingTorrents struct{}

func (failingTorrents) GetTorrents() ([]*transmission.Torrent, error) {
	return nil, errors.New("connection refused")
}
func (failingTorrents) RemoveTorrents([]*transmission.Torrent, bool) error { return nil }

func hasWarning(ws []string, sub string) bool {
	for _, w := range ws {
		if strings.Contains(w, sub) {
			return true
		}
	}
	return false
}

// F74, F78: an unreachable Transmission or Jellyfin does not fail
// list_media; the filesystem items are still listed and the warnings name
// the missing sources.
func TestListMediaReportsUnavailableSources(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "shows", "Some Show"), 0o755); err != nil {
		t.Fatal(err)
	}
	jf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer jf.Close()

	s := &server{
		config: Config{DownloadPath: root},
		scan:   &media.FilesystemScanner{DownloadPath: root},
		trans:  failingTorrents{},
		jf:     media.NewJellyfinClient(jf.URL, "k"),
		abs:    media.NewAudiobookshelfClient("", ""),
	}
	res, out, _ := s.listMedia(context.Background(), nil, listMediaInput{})
	if res.IsError {
		t.Fatalf("list_media failed: %v", res.Content)
	}
	if len(out.Items) != 1 || out.Items[0].Name != "Some Show" {
		t.Fatalf("items = %+v, want the filesystem item", out.Items)
	}
	if !hasWarning(out.Warnings, "transmission unavailable") || !hasWarning(out.Warnings, "jellyfin unavailable") {
		t.Fatalf("warnings = %q, want Transmission and Jellyfin named", out.Warnings)
	}
}

func TestListMediaNoWarningsWhenAllIsWell(t *testing.T) {
	root := t.TempDir()
	s := &server{config: Config{DownloadPath: root}, scan: &media.FilesystemScanner{DownloadPath: root},
		jf: media.NewJellyfinClient("", ""), abs: media.NewAudiobookshelfClient("", "")}
	_, out, _ := s.listMedia(context.Background(), nil, listMediaInput{})
	if len(out.Warnings) != 0 {
		t.Fatalf("warnings = %q, want none", out.Warnings)
	}
}

// F104: a mistyped category is an error naming the valid ones, not an
// empty library.
func TestListMediaRejectsUnknownCategory(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "shows", "Some Show"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := &server{config: Config{DownloadPath: root}, scan: &media.FilesystemScanner{DownloadPath: root},
		jf: media.NewJellyfinClient("", ""), abs: media.NewAudiobookshelfClient("", "")}
	for _, c := range []string{"show", "tv"} {
		res, _, _ := s.listMedia(context.Background(), nil, listMediaInput{Category: c})
		if !res.IsError || !strings.Contains(res.Content[0].(*mcp.TextContent).Text, "shows") {
			t.Errorf("category %q: want an error naming the valid categories, got %+v", c, res.Content)
		}
	}
	for _, c := range []string{"shows", "Shows", "incomplete", ""} {
		if res, _, _ := s.listMedia(context.Background(), nil, listMediaInput{Category: c}); res.IsError {
			t.Errorf("category %q refused", c)
		}
	}
}
