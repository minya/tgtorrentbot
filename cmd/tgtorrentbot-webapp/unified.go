package main

import (
	"slices"
	"strings"
)

// normalizedKey returns a lowercase key used to match items across sources.
func normalizedKey(name, category string) string {
	return strings.ToLower(strings.TrimSpace(name)) + "\x00" + strings.ToLower(strings.TrimSpace(category))
}

// mergeItems combines items from torrents, filesystem, and Jellyfin into a
// unified list. Items are matched by normalized name + category.
func mergeItems(torrents []TorrentInfo, fsItems map[string][]FsItem, incompleteItems []FsItem, jellyfinItems []JellyfinItem) []UnifiedItem {
	type entry struct {
		item  UnifiedItem
		order int // insertion order for stable sort
	}
	merged := make(map[string]*entry)
	nextOrder := 0

	getOrCreate := func(name, category string) *entry {
		key := normalizedKey(name, category)
		if e, ok := merged[key]; ok {
			return e
		}
		e := &entry{
			item: UnifiedItem{
				Name:     name,
				Category: category,
			},
			order: nextOrder,
		}
		nextOrder++
		merged[key] = e
		return e
	}

	// Add torrent items.
	for _, t := range torrents {
		e := getOrCreate(t.Name, t.Category)
		e.item.Sources = appendUnique(e.item.Sources, "torrent")
		id := t.ID
		e.item.TorrentID = &id
		pct := t.PercentDone
		e.item.PercentDone = &pct
		if t.TotalSize > e.item.TotalSize {
			e.item.TotalSize = t.TotalSize
		}
		date := t.AddedDate
		e.item.AddedDate = &date
	}

	// Add filesystem items (per category).
	for category, items := range fsItems {
		for _, fi := range items {
			e := getOrCreate(fi.Name, category)
			e.item.Sources = appendUnique(e.item.Sources, "filesystem")
			if fi.Size > e.item.TotalSize {
				e.item.TotalSize = fi.Size
			}
		}
	}

	// Add incomplete items. Try to match by name with any existing entry;
	// if no match, create a new entry without a category.
	for _, fi := range incompleteItems {
		var matched *entry
		for key, e := range merged {
			nameFromKey := key[:strings.Index(key, "\x00")]
			if strings.ToLower(strings.TrimSpace(fi.Name)) == nameFromKey {
				matched = e
				break
			}
		}
		if matched != nil {
			matched.item.IsIncomplete = true
			matched.item.Sources = appendUnique(matched.item.Sources, "filesystem")
			if fi.Size > matched.item.TotalSize {
				matched.item.TotalSize = fi.Size
			}
		} else {
			e := getOrCreate(fi.Name, "")
			e.item.Sources = appendUnique(e.item.Sources, "filesystem")
			e.item.IsIncomplete = true
			if fi.Size > e.item.TotalSize {
				e.item.TotalSize = fi.Size
			}
		}
	}

	// Add Jellyfin items.
	for _, ji := range jellyfinItems {
		e := getOrCreate(ji.Name, ji.Category)
		e.item.Sources = appendUnique(e.item.Sources, "jellyfin")
	}

	// Collect results ordered by insertion order.
	result := make([]UnifiedItem, 0, len(merged))
	ordered := make([]*entry, 0, len(merged))
	for _, e := range merged {
		ordered = append(ordered, e)
	}
	// Sort by insertion order to keep torrent items first (stable).
	for i := 0; i < len(ordered); i++ {
		for j := i + 1; j < len(ordered); j++ {
			if ordered[j].order < ordered[i].order {
				ordered[i], ordered[j] = ordered[j], ordered[i]
			}
		}
	}
	for _, e := range ordered {
		result = append(result, e.item)
	}
	return result
}

func appendUnique(slice []string, val string) []string {
	if slices.Contains(slice, val) {
		return slice
	}
	return append(slice, val)
}
