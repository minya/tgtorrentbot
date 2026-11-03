package main

import (
	"context"
	"fmt"
	"github.com/minya/logger"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/minya/tgtorrentbot/internal/category"
	mcplib "github.com/minya/tgtorrentbot/internal/media"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// knownCategories are the category directories list_media scans, derived from
// internal/category (the single source of truth).
var knownCategories = category.Names()

// maxListedFiles caps the total number of files returned by a recursive
// list_media call so an unfiltered request can't produce a giant response.
const maxListedFiles = 5000

type listMediaInput struct {
	Category  string `json:"category,omitempty" jsonschema:"optional category filter; one of the categories named in the tool description, or 'incomplete' for orphaned leftovers in the incomplete directory"`
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
	// Warnings names every source that could not be read, so a missing
	// "torrent", "jellyfin" or "audiobookshelf" source on an item is not
	// mistaken for "that service does not know this item".
	Warnings []string `json:"warnings,omitempty"`
}

// listMediaSourceTimeout bounds how long list_media waits for Jellyfin and
// Audiobookshelf; a variable so tests can shorten it.
var listMediaSourceTimeout = 15 * time.Second

func (s *server) listMedia(ctx context.Context, _ *mcp.CallToolRequest, in listMediaInput) (*mcp.CallToolResult, listMediaOutput, error) {
	// Torrents (optional — runs without transmission).
	// Every source is optional: one that fails is skipped and reported in
	// warnings, so the rest of the library stays listable (e.g. paths for
	// read_tags or move_media while Transmission is stopped).
	var warnings []string
	warn := func(msg string) {
		logger.Warn("list_media: %s", msg)
		warnings = append(warnings, msg)
	}

	var torrents []mcplib.TorrentInfo
	if s.trans != nil {
		rawTorrents, err := s.trans.GetTorrents()
		if err != nil {
			warn(fmt.Sprintf("transmission unavailable, torrents not listed: %v", err))
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
		if err != nil {
			warn(fmt.Sprintf("cannot read the %s directory: %v", cat, err))
		}
		if len(items) > 0 {
			fsItems[cat] = items
		}
	}
	incompleteItems, err := s.scan.ScanIncomplete()
	if err != nil {
		warn(fmt.Sprintf("cannot read the incomplete directory: %v", err))
	}

	// Jellyfin + Audiobookshelf: optional, fetched concurrently and bounded
	// so a hung server cannot hold the tool call (or keep running after it).
	srcCtx, cancel := context.WithTimeout(ctx, listMediaSourceTimeout)
	jfItems, absItems, srcErrs := mcplib.FetchLibraries(srcCtx, s.jf, s.abs)
	cancel()
	for _, err := range srcErrs {
		warnings = append(warnings, err.Error()+"; its source is missing from every item") // already logged
	}

	merged := mcplib.MergeItems(torrents, fsItems, incompleteItems, jfItems, absItems)

	wantCat := strings.ToLower(strings.TrimSpace(in.Category))
	wantQuery := strings.ToLower(strings.TrimSpace(in.Query))

	out := listMediaOutput{Items: make([]listMediaItem, 0, len(merged)), Warnings: warnings}
	for _, item := range merged {
		if wantCat != "" && item.Category != wantCat {
			continue
		}
		if wantQuery != "" && !strings.Contains(strings.ToLower(item.Name), wantQuery) {
			continue
		}
		// The on-disk location the scan found: the completed copy when there
		// is one, else the incomplete directory (in-progress downloads and
		// orphans of the virtual incomplete category).
		var path string
		if len(item.Paths) > 0 {
			path = item.Paths[0]
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
