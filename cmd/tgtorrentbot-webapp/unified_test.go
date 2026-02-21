package main

import (
	"slices"
	"testing"
)

func TestMergeItems_AllThreeSources(t *testing.T) {
	torrents := []TorrentInfo{
		{ID: 1, Name: "MyMovie", PercentDone: 100, Category: "movies", TotalSize: 1000, AddedDate: 111},
	}
	fsItems := map[string][]FsItem{
		"movies": {{Name: "MyMovie", Size: 1000}},
	}
	jellyfinItems := []JellyfinItem{
		{Name: "MyMovie", Category: "movies", JellyfinID: "jf-1"},
	}

	result := mergeItems(torrents, fsItems, nil, jellyfinItems)
	if len(result) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result))
	}
	item := result[0]
	if item.Name != "MyMovie" {
		t.Errorf("expected name MyMovie, got %s", item.Name)
	}
	if item.Category != "movies" {
		t.Errorf("expected category movies, got %s", item.Category)
	}
	if len(item.Sources) != 3 {
		t.Errorf("expected 3 sources, got %d: %v", len(item.Sources), item.Sources)
	}
	for _, src := range []string{"torrent", "filesystem", "jellyfin"} {
		if !slices.Contains(item.Sources, src) {
			t.Errorf("expected source %s to be present", src)
		}
	}
	if item.TorrentID == nil || *item.TorrentID != 1 {
		t.Errorf("expected torrentId 1, got %v", item.TorrentID)
	}
	if item.PercentDone == nil || *item.PercentDone != 100 {
		t.Errorf("expected percentDone 100, got %v", item.PercentDone)
	}
}

func TestMergeItems_OnlyTorrent(t *testing.T) {
	torrents := []TorrentInfo{
		{ID: 5, Name: "Show1", PercentDone: 50, Category: "shows", TotalSize: 2000, AddedDate: 222},
	}
	result := mergeItems(torrents, nil, nil, nil)
	if len(result) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result))
	}
	item := result[0]
	if len(item.Sources) != 1 || item.Sources[0] != "torrent" {
		t.Errorf("expected sources [torrent], got %v", item.Sources)
	}
	if item.TorrentID == nil || *item.TorrentID != 5 {
		t.Errorf("expected torrentId 5")
	}
}

func TestMergeItems_OnlyFilesystem(t *testing.T) {
	fsItems := map[string][]FsItem{
		"music": {{Name: "Album1", Size: 500}},
	}
	result := mergeItems(nil, fsItems, nil, nil)
	if len(result) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result))
	}
	item := result[0]
	if item.Name != "Album1" {
		t.Errorf("expected Album1, got %s", item.Name)
	}
	if item.Category != "music" {
		t.Errorf("expected music, got %s", item.Category)
	}
	if len(item.Sources) != 1 || item.Sources[0] != "filesystem" {
		t.Errorf("expected sources [filesystem], got %v", item.Sources)
	}
	if item.TorrentID != nil {
		t.Errorf("expected nil torrentId")
	}
	if item.TotalSize != 500 {
		t.Errorf("expected size 500, got %d", item.TotalSize)
	}
}

func TestMergeItems_OnlyJellyfin(t *testing.T) {
	jellyfinItems := []JellyfinItem{
		{Name: "JellyMovie", Category: "movies", JellyfinID: "jf-10"},
	}
	result := mergeItems(nil, nil, nil, jellyfinItems)
	if len(result) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result))
	}
	item := result[0]
	if len(item.Sources) != 1 || item.Sources[0] != "jellyfin" {
		t.Errorf("expected sources [jellyfin], got %v", item.Sources)
	}
}

func TestMergeItems_IncompleteMatchesTorrent(t *testing.T) {
	torrents := []TorrentInfo{
		{ID: 3, Name: "Downloading", PercentDone: 40, Category: "movies", TotalSize: 5000, AddedDate: 333},
	}
	incompleteItems := []FsItem{
		{Name: "Downloading", Size: 2000, IsIncomplete: true},
	}
	result := mergeItems(torrents, nil, incompleteItems, nil)
	if len(result) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result))
	}
	item := result[0]
	if !item.IsIncomplete {
		t.Error("expected IsIncomplete to be true")
	}
	if len(item.Sources) != 2 {
		t.Errorf("expected 2 sources (torrent+filesystem), got %v", item.Sources)
	}
}

func TestMergeItems_IncompleteNoMatch(t *testing.T) {
	incompleteItems := []FsItem{
		{Name: "OrphanedDownload", Size: 1500, IsIncomplete: true},
	}
	result := mergeItems(nil, nil, incompleteItems, nil)
	if len(result) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result))
	}
	item := result[0]
	if item.Name != "OrphanedDownload" {
		t.Errorf("expected OrphanedDownload, got %s", item.Name)
	}
	if item.Category != "others" {
		t.Errorf("expected others category, got %s", item.Category)
	}
	if !item.IsIncomplete {
		t.Error("expected IsIncomplete to be true")
	}
}

func TestMergeItems_CaseInsensitiveMatching(t *testing.T) {
	torrents := []TorrentInfo{
		{ID: 1, Name: "My Movie", Category: "movies", TotalSize: 1000},
	}
	fsItems := map[string][]FsItem{
		"movies": {{Name: "my movie", Size: 1000}},
	}
	result := mergeItems(torrents, fsItems, nil, nil)
	if len(result) != 1 {
		t.Fatalf("expected 1 item (case-insensitive match), got %d", len(result))
	}
	if len(result[0].Sources) != 2 {
		t.Errorf("expected 2 sources, got %v", result[0].Sources)
	}
}

func TestMergeItems_DifferentCategories_NotMerged(t *testing.T) {
	torrents := []TorrentInfo{
		{ID: 1, Name: "Item", Category: "movies", TotalSize: 1000},
	}
	fsItems := map[string][]FsItem{
		"music": {{Name: "Item", Size: 500}},
	}
	result := mergeItems(torrents, fsItems, nil, nil)
	if len(result) != 2 {
		t.Fatalf("expected 2 items (different categories), got %d", len(result))
	}
}

func TestMergeItems_EmptyInputs(t *testing.T) {
	result := mergeItems(nil, nil, nil, nil)
	if len(result) != 0 {
		t.Fatalf("expected 0 items, got %d", len(result))
	}
}

func TestMergeItems_MultipleItems(t *testing.T) {
	torrents := []TorrentInfo{
		{ID: 1, Name: "Movie1", Category: "movies", TotalSize: 1000},
		{ID: 2, Name: "Movie2", Category: "movies", TotalSize: 2000},
	}
	fsItems := map[string][]FsItem{
		"movies": {{Name: "Movie1", Size: 1000}, {Name: "Movie3", Size: 3000}},
	}
	jellyfinItems := []JellyfinItem{
		{Name: "Movie2", Category: "movies", JellyfinID: "jf-2"},
	}
	result := mergeItems(torrents, fsItems, nil, jellyfinItems)
	if len(result) != 3 {
		t.Fatalf("expected 3 items, got %d", len(result))
	}

	// Build a map for easier lookup
	byName := make(map[string]UnifiedItem)
	for _, item := range result {
		byName[item.Name] = item
	}

	// Movie1: torrent + filesystem
	m1 := byName["Movie1"]
	if len(m1.Sources) != 2 {
		t.Errorf("Movie1: expected 2 sources, got %v", m1.Sources)
	}
	if !slices.Contains(m1.Sources, "torrent") || !slices.Contains(m1.Sources, "filesystem") {
		t.Errorf("Movie1: expected torrent+filesystem, got %v", m1.Sources)
	}

	// Movie2: torrent + jellyfin
	m2 := byName["Movie2"]
	if len(m2.Sources) != 2 {
		t.Errorf("Movie2: expected 2 sources, got %v", m2.Sources)
	}
	if !slices.Contains(m2.Sources, "torrent") || !slices.Contains(m2.Sources, "jellyfin") {
		t.Errorf("Movie2: expected torrent+jellyfin, got %v", m2.Sources)
	}

	// Movie3: filesystem only
	m3 := byName["Movie3"]
	if len(m3.Sources) != 1 || m3.Sources[0] != "filesystem" {
		t.Errorf("Movie3: expected [filesystem], got %v", m3.Sources)
	}
	if m3.TotalSize != 3000 {
		t.Errorf("Movie3: expected size 3000, got %d", m3.TotalSize)
	}
}

func TestMergeItems_SizePrecedence(t *testing.T) {
	torrents := []TorrentInfo{
		{ID: 1, Name: "BigMovie", Category: "movies", TotalSize: 1000},
	}
	fsItems := map[string][]FsItem{
		"movies": {{Name: "BigMovie", Size: 1500}},
	}
	result := mergeItems(torrents, fsItems, nil, nil)
	if len(result) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result))
	}
	if result[0].TotalSize != 1500 {
		t.Errorf("expected larger size 1500, got %d", result[0].TotalSize)
	}
}

func TestAppendUnique(t *testing.T) {
	s := appendUnique(nil, "a")
	if len(s) != 1 || s[0] != "a" {
		t.Errorf("expected [a], got %v", s)
	}
	s = appendUnique(s, "a")
	if len(s) != 1 {
		t.Errorf("expected no duplicate, got %v", s)
	}
	s = appendUnique(s, "b")
	if len(s) != 2 {
		t.Errorf("expected [a b], got %v", s)
	}
}

func TestNormalizedKey(t *testing.T) {
	k1 := normalizedKey("My Movie", "movies")
	k2 := normalizedKey("my movie", "Movies")
	if k1 != k2 {
		t.Errorf("expected keys to match: %q vs %q", k1, k2)
	}
	k3 := normalizedKey("  My Movie  ", "  movies  ")
	if k1 != k3 {
		t.Errorf("expected trimmed key to match: %q vs %q", k1, k3)
	}
}
