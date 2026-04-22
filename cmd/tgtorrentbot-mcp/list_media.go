package main

import (
	"context"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	mcplib "github.com/minya/tgtorrentbot/internal/media"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var knownCategories = []string{"movies", "shows", "music", "musicvideos", "audiobooks", "others"}

type listMediaInput struct {
	Category string `json:"category,omitempty" jsonschema:"optional category filter: movies, shows, music, musicvideos, audiobooks, or others"`
	Query    string `json:"query,omitempty" jsonschema:"optional case-insensitive substring to match against item names"`
}

type listMediaItem struct {
	Name         string   `json:"name"`
	Category     string   `json:"category"`
	Path         string   `json:"path,omitempty"`
	Sources      []string `json:"sources"`
	TotalSize    int64    `json:"total_size"`
	TorrentID    *int     `json:"torrent_id,omitempty"`
	PercentDone  *float64 `json:"percent_done,omitempty"`
	IsIncomplete bool     `json:"is_incomplete,omitempty"`
}

type listMediaOutput struct {
	Items []listMediaItem `json:"items"`
}

func (s *server) listMedia(ctx context.Context, _ *mcp.CallToolRequest, in listMediaInput) (*mcp.CallToolResult, listMediaOutput, error) {
	// Torrents (optional — runs without transmission).
	var torrents []mcplib.TorrentInfo
	if s.trans != nil {
		rawTorrents, err := s.trans.GetTorrents()
		if err != nil {
			return formatError("failed to fetch torrents: %v", err), listMediaOutput{}, nil
		}
		for _, t := range rawTorrents {
			category := "others"
			if len(t.Labels) >= 2 {
				category = t.Labels[1]
			}
			torrents = append(torrents, mcplib.TorrentInfo{
				ID:          t.ID,
				Name:        t.Name,
				PercentDone: t.PercentDone * 100,
				Category:    category,
				TotalSize:   t.TotalSize,
			})
		}
	}

	// Filesystem.
	fsItems := make(map[string][]mcplib.FsItem)
	for _, cat := range knownCategories {
		items, err := s.scan.ScanCategory(cat)
		if err == nil && len(items) > 0 {
			fsItems[cat] = items
		}
	}
	incompleteItems, _ := s.scan.ScanIncomplete()

	// Jellyfin + Audiobookshelf.
	jfItems, _ := s.jf.GetItems()
	absItems, _ := s.abs.GetItems()

	merged := mcplib.MergeItems(torrents, fsItems, incompleteItems, jfItems, absItems)

	wantCat := strings.ToLower(strings.TrimSpace(in.Category))
	wantQuery := strings.ToLower(strings.TrimSpace(in.Query))

	out := listMediaOutput{Items: make([]listMediaItem, 0, len(merged))}
	for _, item := range merged {
		if wantCat != "" && item.Category != wantCat {
			continue
		}
		if wantQuery != "" && !strings.Contains(strings.ToLower(item.Name), wantQuery) {
			continue
		}
		// Resolve filesystem path if the item exists on disk.
		var path string
		if slices.Contains(item.Sources, "filesystem") {
			path = filepath.Join(s.config.DownloadPath, item.Category, item.Name)
		}
		out.Items = append(out.Items, listMediaItem{
			Name:         item.Name,
			Category:     item.Category,
			Path:         path,
			Sources:      item.Sources,
			TotalSize:    item.TotalSize,
			TorrentID:    item.TorrentID,
			PercentDone:  item.PercentDone,
			IsIncomplete: item.IsIncomplete,
		})
	}

	sort.SliceStable(out.Items, func(i, j int) bool {
		if out.Items[i].Category != out.Items[j].Category {
			return out.Items[i].Category < out.Items[j].Category
		}
		return strings.ToLower(out.Items[i].Name) < strings.ToLower(out.Items[j].Name)
	})

	// Serialize the items into the text content too: MCP clients that ignore
	// structured_content (e.g. claude.ai mobile) otherwise see only the count.
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: mustJSON(out)}},
	}, out, nil
}
