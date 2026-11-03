package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/minya/tgtorrentbot/internal/media"
)

// F32: a refresh that the server rejects is reported as a tool error, not
// as "triggered".
func TestJellyfinRescanReportsFailure(t *testing.T) {
	for _, tc := range []struct {
		status  int
		wantErr bool
	}{{http.StatusUnauthorized, true}, {http.StatusInternalServerError, true}, {http.StatusNoContent, false}} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status) }))
		s := &server{config: Config{JellyfinURL: srv.URL, JellyfinAPIKey: "k"}, jf: media.NewJellyfinClient(srv.URL, "k")}
		res, out, _ := s.jellyfinRescan(context.Background(), nil, jellyfinRescanInput{})
		srv.Close()
		if res.IsError != tc.wantErr || out.Triggered == tc.wantErr {
			t.Errorf("status %d: IsError=%v Triggered=%v, want IsError=%v", tc.status, res.IsError, out.Triggered, tc.wantErr)
		}
	}
}

func TestJellyfinRescanUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	s := &server{config: Config{JellyfinURL: url, JellyfinAPIKey: "k"}, jf: media.NewJellyfinClient(url, "k")}
	if res, _, _ := s.jellyfinRescan(context.Background(), nil, jellyfinRescanInput{}); !res.IsError {
		t.Fatal("unreachable Jellyfin reported as triggered")
	}
}

func absScanServer(t *testing.T, listStatus, scanStatus int) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/libraries":
			w.WriteHeader(listStatus)
			w.Write([]byte(`{"libraries":[{"id":"lib_a","name":"Books","mediaType":"book"},{"id":"lib_b","name":"Pods","mediaType":"podcast"}]}`))
		case strings.HasSuffix(r.URL.Path, "/scan"):
			if strings.Contains(r.URL.Path, "lib_b") {
				w.WriteHeader(scanStatus)
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestAbsRescanReportsFailure(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		listStatus, scanStatus int
		wantErr                bool
		wantLibs               int
	}{
		{"forbidden key", http.StatusForbidden, http.StatusOK, true, 0},
		{"one library fails", http.StatusOK, http.StatusInternalServerError, true, 1},
		{"all scanned", http.StatusOK, http.StatusOK, false, 2},
	} {
		url := absScanServer(t, tc.listStatus, tc.scanStatus)
		s := &server{config: Config{AudiobookshelfURL: url, AudiobookshelfAPIKey: "k"}, abs: media.NewAudiobookshelfClient(url, "k")}
		res, out, _ := s.absRescan(context.Background(), nil, absRescanInput{})
		if res.IsError != tc.wantErr || out.Triggered == tc.wantErr || len(out.Libraries) != tc.wantLibs {
			t.Errorf("%s: IsError=%v out=%+v, want IsError=%v and %d libraries", tc.name, res.IsError, out, tc.wantErr, tc.wantLibs)
		}
	}
}

// F43: list_media answers when Jellyfin and ABS hang, within its deadline.
func TestListMediaBoundedWhenSourcesHang(t *testing.T) {
	old := listMediaSourceTimeout
	listMediaSourceTimeout = 200 * time.Millisecond
	t.Cleanup(func() { listMediaSourceTimeout = old })
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-done:
		}
	}))
	t.Cleanup(func() { close(done); srv.Close() })

	root := t.TempDir()
	s := &server{
		config: Config{DownloadPath: root},
		scan:   &media.FilesystemScanner{DownloadPath: root},
		jf:     media.NewJellyfinClient(srv.URL, "k"),
		abs:    media.NewAudiobookshelfClient(srv.URL, "k"),
	}
	start := time.Now()
	res, _, _ := s.listMedia(context.Background(), nil, listMediaInput{})
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("list_media took %s with hung sources", took)
	}
	if res.IsError {
		t.Fatalf("list_media failed: %v", res.Content)
	}
}
