package main

import (
	"context"
	"io/fs"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	mcplib "github.com/minya/tgtorrentbot/internal/media"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var knownCategories = []string{"movies", "shows", "music", "musicvideos", "audiobooks", "others"}

// maxListedFiles caps the total number of files returned by a recursive
// list_media call so an unfiltered request can't produce a giant response.
const maxListedFiles = 5000

type listMediaInput struct {
	Category  string `json:"category,omitempty" jsonschema:"optional category filter: movies, shows, music, musicvideos, audiobooks, or others"`
	Query     string `json:"query,omitempty" jsonschema:"optional case-insensitive substring to match against item names"`
	Recursive bool   `json:"recursive,omitempty" jsonschema:"when true, also list every file inside each item's directory (e.g. individual episodes of a show) under 'files'; combine with category/query to keep the output small"`
}

type listMediaFile struct {
	Path    string `json:"path"` // same form as the item's path; usable as-is in move_media/read_tags
	Size    int64  `json:"size"`
	ModTime string `json:"mtime"`
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
	// AddedDate is when Transmission added the torrent; ModTime is the newest
	// filesystem mtime of the item's top-level entry. Both RFC 3339 UTC.
	AddedDate string          `json:"added_date,omitempty"`
	ModTime   string          `json:"mtime,omitempty"`
	Files     []listMediaFile `json:"files,omitempty"`
}

type listMediaOutput struct {
	Items          []listMediaItem `json:"items"`
	FilesTruncated bool            `json:"files_truncated,omitempty"`
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
				AddedDate:   t.AddedDate,
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
		li := listMediaItem{
			Name:         item.Name,
			Category:     item.Category,
			Path:         path,
			Sources:      item.Sources,
			TotalSize:    item.TotalSize,
			TorrentID:    item.TorrentID,
			PercentDone:  item.PercentDone,
			IsIncomplete: item.IsIncomplete,
		}
		if item.AddedDate != nil && *item.AddedDate > 0 {
			li.AddedDate = formatUnix(int64(*item.AddedDate))
		}
		if item.ModTime != nil {
			li.ModTime = formatUnix(*item.ModTime)
		}
		out.Items = append(out.Items, li)
	}

	sort.SliceStable(out.Items, func(i, j int) bool {
		if out.Items[i].Category != out.Items[j].Category {
			return out.Items[i].Category < out.Items[j].Category
		}
		return strings.ToLower(out.Items[i].Name) < strings.ToLower(out.Items[j].Name)
	})

	if in.Recursive {
		budget := maxListedFiles
		for i := range out.Items {
			if out.Items[i].Path == "" {
				continue
			}
			files, truncated := listFiles(out.Items[i].Path, &budget)
			out.Items[i].Files = files
			if truncated {
				out.FilesTruncated = true
				break
			}
		}
	}

	// Serialize the items into the text content too: MCP clients that ignore
	// structured_content (e.g. claude.ai mobile) otherwise see only the count.
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: mustJSON(out)}},
	}, out, nil
}

// listFiles walks root and returns its regular files (root itself when it is a
// single file), decrementing *budget per file. It stops and reports
// truncated=true when the budget runs out.
func listFiles(root string, budget *int) (files []listMediaFile, truncated bool) {
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		if *budget <= 0 {
			truncated = true
			return filepath.SkipAll
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		files = append(files, listMediaFile{
			Path:    p,
			Size:    info.Size(),
			ModTime: formatUnix(info.ModTime().Unix()),
		})
		*budget--
		return nil
	})
	return files, truncated
}

func formatUnix(sec int64) string {
	return time.Unix(sec, 0).UTC().Format(time.RFC3339)
}
