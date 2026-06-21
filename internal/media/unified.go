package media

import (
	"slices"
	"sort"
	"strings"
)

// IncompleteCategory is a virtual category for files left in the incomplete
// directory that no torrent owns (e.g. a stalled download whose torrent was
// removed). It is not a real download category: nothing can be downloaded into
// it, but its items can be deleted to clean up the leftover data.
const IncompleteCategory = "incomplete"

// normalizedKey returns a lowercase key used to match items across sources.
func normalizedKey(name, category string) string {
	return strings.ToLower(strings.TrimSpace(name)) + "\x00" + strings.ToLower(strings.TrimSpace(category))
}

// MergeItems combines items from torrents, filesystem, Jellyfin, and
// Audiobookshelf into a unified list. Items are matched by normalized
// name + category.
func MergeItems(
	torrents []TorrentInfo,
	fsItems map[string][]FsItem,
	incompleteItems []FsItem,
	jellyfinItems []JellyfinItem,
	absItems []AudiobookshelfItem,
) []UnifiedItem {
	type entry struct {
		item  UnifiedItem
		order int
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
		rate := t.RateDownload
		e.item.RateDownload = &rate
		eta := t.Eta
		e.item.Eta = &eta
		peers := t.PeersConnected
		e.item.PeersConnected = &peers
		seeds := t.PeersSendingToUs
		e.item.PeersSendingToUs = &seeds
	}

	for category, items := range fsItems {
		for _, fi := range items {
			e := getOrCreate(fi.Name, category)
			e.item.Sources = appendUnique(e.item.Sources, "filesystem")
			if fi.Size > e.item.TotalSize {
				e.item.TotalSize = fi.Size
			}
		}
	}

	nameIndex := make(map[string]*entry)
	for key, e := range merged {
		normName := key[:strings.Index(key, "\x00")]
		if existing, ok := nameIndex[normName]; !ok {
			nameIndex[normName] = e
		} else {
			eSrc := slices.Contains(e.item.Sources, "torrent")
			existSrc := slices.Contains(existing.item.Sources, "torrent")
			if eSrc && !existSrc {
				nameIndex[normName] = e
			} else if eSrc == existSrc && e.item.Category < existing.item.Category {
				nameIndex[normName] = e
			}
		}
	}
	for _, fi := range incompleteItems {
		normName := strings.ToLower(strings.TrimSpace(fi.Name))
		if matched, ok := nameIndex[normName]; ok {
			matched.item.IsIncomplete = true
			matched.item.Sources = appendUnique(matched.item.Sources, "filesystem")
			if fi.Size > matched.item.TotalSize {
				matched.item.TotalSize = fi.Size
			}
		} else {
			// No torrent and no completed directory owns this incomplete
			// folder — it's orphaned leftover data. Surface it under the
			// virtual "incomplete" category so it can be deleted, rather
			// than masquerading as real "others" media.
			e := getOrCreate(fi.Name, IncompleteCategory)
			e.item.Sources = appendUnique(e.item.Sources, "filesystem")
			e.item.IsIncomplete = true
			if fi.Size > e.item.TotalSize {
				e.item.TotalSize = fi.Size
			}
		}
	}

	for _, ji := range jellyfinItems {
		e := getOrCreate(ji.Name, ji.Category)
		e.item.Sources = appendUnique(e.item.Sources, "jellyfin")
	}

	for _, ai := range absItems {
		normName := strings.ToLower(strings.TrimSpace(ai.Name))
		normCat := strings.ToLower(strings.TrimSpace(ai.Category))
		var matched *entry
		for key, e := range merged {
			parts := strings.SplitN(key, "\x00", 2)
			if len(parts) != 2 || parts[1] != normCat {
				continue
			}
			if normName == parts[0] || strings.HasPrefix(normName, parts[0]+"/") {
				matched = e
				break
			}
		}
		if matched != nil {
			matched.item.Sources = appendUnique(matched.item.Sources, "audiobookshelf")
		} else {
			e := getOrCreate(ai.Name, ai.Category)
			e.item.Sources = appendUnique(e.item.Sources, "audiobookshelf")
		}
	}

	result := make([]UnifiedItem, 0, len(merged))
	ordered := make([]*entry, 0, len(merged))
	for _, e := range merged {
		ordered = append(ordered, e)
	}
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].order < ordered[j].order
	})
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
