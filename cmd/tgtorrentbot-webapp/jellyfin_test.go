package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetItems(t *testing.T) {
	resp := jellyfinResponse{
		Items: []jellyfinResponseItem{
			{Name: "The Matrix", ID: "abc123", Path: "/media/movies/The Matrix/The Matrix.mkv"},
			{Name: "Breaking Bad", ID: "def456", Path: "/media/shows/Breaking Bad/Season 1/ep1.mkv"},
			{Name: "Song", ID: "ghi789", Path: "/media/music/Song/track.flac"},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/Items" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("Recursive") != "true" {
			t.Error("expected Recursive=true")
		}
		auth := r.Header.Get("Authorization")
		if auth != `MediaBrowser Token="testkey"` {
			t.Errorf("unexpected auth header: %s", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := newJellyfinClient(srv.URL, "testkey")
	items, err := client.GetItems()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}

	// Check first item
	if items[0].Name != "The Matrix" {
		t.Errorf("expected The Matrix, got %s", items[0].Name)
	}
	if items[0].Category != "movies" {
		t.Errorf("expected movies, got %s", items[0].Category)
	}
	if items[0].JellyfinID != "abc123" {
		t.Errorf("expected abc123, got %s", items[0].JellyfinID)
	}

	// Check second item
	if items[1].Category != "shows" {
		t.Errorf("expected shows, got %s", items[1].Category)
	}

	// Check third item
	if items[2].Category != "music" {
		t.Errorf("expected music, got %s", items[2].Category)
	}
}

func TestGetItemsNotConfigured(t *testing.T) {
	// Empty URL
	client := newJellyfinClient("", "somekey")
	items, err := client.GetItems()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if items != nil {
		t.Fatalf("expected nil, got %v", items)
	}

	// Empty API key
	client = newJellyfinClient("http://localhost:8096", "")
	items, err = client.GetItems()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if items != nil {
		t.Fatalf("expected nil, got %v", items)
	}
}

func TestGetItemsServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := newJellyfinClient(srv.URL, "testkey")
	_, err := client.GetItems()
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestGetItemsEmptyLibrary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jellyfinResponse{Items: []jellyfinResponseItem{}})
	}))
	defer srv.Close()

	client := newJellyfinClient(srv.URL, "testkey")
	items, err := client.GetItems()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 items, got %d", len(items))
	}
}

func TestCategoryFromPath(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/media/movies/The Matrix/file.mkv", "movies"},
		{"/media/shows/Breaking Bad/Season 1/ep.mkv", "shows"},
		{"/media/music/Artist/Album/track.flac", "music"},
		{"/media/audiobooks/Book/chapter1.mp3", "audiobooks"},
		{"/media/musicvideos/Video/clip.mp4", "musicvideos"},
		{"/media/others/Misc/file.bin", "others"},
		{"/some/other/path/file.mkv", ""},
		{"", ""},
		{"/media/", ""},
	}

	for _, tt := range tests {
		got := categoryFromPath(tt.path)
		if got != tt.expected {
			t.Errorf("categoryFromPath(%q) = %q, want %q", tt.path, got, tt.expected)
		}
	}
}

func TestGetItemsTrailingSlashURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jellyfinResponse{Items: []jellyfinResponseItem{
			{Name: "Test", ID: "t1", Path: "/media/movies/Test/file.mkv"},
		}})
	}))
	defer srv.Close()

	// URL with trailing slash should still work
	client := newJellyfinClient(srv.URL+"/", "testkey")
	items, err := client.GetItems()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
}
