# Unified Media Items: Transmission + Filesystem + Jellyfin

## Overview

Modify the webapp to display a unified view of media items from three sources: Transmission torrents, filesystem directories, and Jellyfin library items. Items are matched by name across sources, and each item shows which sources it appears in. This gives a complete picture of all media, including orphaned files (torrent deleted but files remain), moved folders, and items stuck in the incomplete directory.

## Context

- Files involved:
  - `cmd/tgtorrentbot-webapp/main.go` (config, handlers, new endpoints)
  - `cmd/tgtorrentbot-webapp/payloads.go` (response types)
  - `cmd/tgtorrentbot-webapp/static/index.html` (frontend)
  - `docker-compose.yaml` (new env vars for webapp)
- The webapp container already has `/downloads` mounted, so filesystem scanning works out of the box
- Jellyfin is on the same Docker network (`tunnel-net`) at `tgt-jellyfin:8096`
- Jellyfin API key must be generated from Jellyfin admin dashboard
- Related patterns: existing handler pattern in `main.go`, existing payload types in `payloads.go`

## Matching Strategy

Items from different sources are matched by **normalized name** (case-insensitive, trimmed). A transmission torrent name, a filesystem directory name, and a Jellyfin item name that match are merged into a single unified item.

Category is determined by:
- Torrent: from labels[1]
- Filesystem: from parent directory name (e.g., `/downloads/movies/SomeMovie` -> `movies`)
- Jellyfin: from item path prefix (e.g., `/media/movies/SomeMovie` -> `movies`)

The incomplete directory (`/downloads/incomplete/`) is also scanned. Items found only there get a special "incomplete" indicator.

## Development Approach

- **Testing approach**: Regular (code first, then tests)
- Complete each task fully before moving to the next
- **CRITICAL: every task MUST include new/updated tests**
- **CRITICAL: all tests must pass before starting next task**

## Implementation Steps

### Task 1: Add Jellyfin and filesystem configuration

**Files:**
- Modify: `cmd/tgtorrentbot-webapp/main.go`
- Modify: `docker-compose.yaml`

- [x] Add `JellyfinURL` and `JellyfinAPIKey` fields to `Config` struct
- [x] Read `TGT_JELLYFIN_URL` and `TGT_JELLYFIN_API_KEY` env vars in `main()` (optional - webapp works without them)
- [x] Add `IncompletePath` field to Config (defaults to `{DownloadPath}/../incomplete` which matches Transmission's default layout under `/downloads`)
- [x] Add env vars to webapp service in `docker-compose.yaml`: `TGT_JELLYFIN_URL=http://tgt-jellyfin:8096`, `TGT_JELLYFIN_API_KEY=${JELLYFIN_API_KEY}`
- [x] Write tests for config loading

### Task 2: Implement filesystem scanner

**Files:**
- Create: `cmd/tgtorrentbot-webapp/filesystem.go`

- [x] Create `filesystemScanner` struct that takes download path and incomplete path
- [x] Implement `ScanCategory(category string) ([]FsItem, error)` - lists subdirectories in `{downloadPath}/{category}/`, returns name + size
- [x] Implement `ScanIncomplete() ([]FsItem, error)` - lists subdirectories in incomplete path
- [x] `FsItem` struct: `Name string`, `Size int64`, `IsIncomplete bool`
- [x] Write tests using a temp directory structure

### Task 3: Implement Jellyfin API client

**Files:**
- Create: `cmd/tgtorrentbot-webapp/jellyfin.go`

- [x] Create `jellyfinClient` struct with URL and API key
- [x] Implement `GetItems() ([]JellyfinItem, error)` - calls `GET /Items?Recursive=true&Fields=Path,MediaSources` with API key header `Authorization: MediaBrowser Token="{key}"`
- [x] Parse response to extract item name, path, and ID
- [x] Determine category from path prefix (e.g., `/media/movies/...` -> `movies`)
- [x] `JellyfinItem` struct: `Name string`, `Category string`, `JellyfinID string`
- [x] Handle case where Jellyfin is not configured (return empty list)
- [x] Write tests with HTTP test server

### Task 4: Create unified items API endpoint

**Files:**
- Modify: `cmd/tgtorrentbot-webapp/main.go` (register route)
- Create: `cmd/tgtorrentbot-webapp/unified.go` (merge logic)
- Modify: `cmd/tgtorrentbot-webapp/payloads.go` (new types)

- [ ] Define `UnifiedItem` struct:
  ```go
  type UnifiedItem struct {
      Name        string   `json:"name"`
      Category    string   `json:"category"`
      Sources     []string `json:"sources"`      // e.g. ["torrent", "filesystem", "jellyfin"]
      TorrentID   *int     `json:"torrentId,omitempty"`
      PercentDone *float64 `json:"percentDone,omitempty"`
      TotalSize   int64    `json:"totalSize"`
      AddedDate   *int     `json:"addedDate,omitempty"`
      IsIncomplete bool    `json:"isIncomplete,omitempty"`
  }
  ```
- [ ] Implement merge function: collect items from all 3 sources, match by normalized name + category, merge sources list
- [ ] Items from incomplete dir: set `IsIncomplete: true`, try to match with torrent by name
- [ ] Register `GET /api/items` endpoint that returns `[]UnifiedItem`
- [ ] Keep existing `/api/torrents` endpoint unchanged (bot still uses it for downloads)
- [ ] Write tests for merge logic with various combinations (item in all 3 sources, item in only 1 source, incomplete items)

### Task 5: Update frontend to use unified items

**Files:**
- Modify: `cmd/tgtorrentbot-webapp/static/index.html`

- [ ] Change `loadMainScreen()` to fetch from `/api/items` instead of `/api/torrents`
- [ ] Update `renderTorrentItem()` to show source badges (small colored dots or tags: T=torrent, F=filesystem, J=jellyfin)
- [ ] Show "incomplete" indicator for items stuck in incomplete directory
- [ ] Update category counts to use unified items
- [ ] Update `refreshCategoryView()` to use `/api/items`
- [ ] Keep remove button only for items that have a torrent source (have `torrentId`)
- [ ] Items without torrent source should not have remove button

### Task 6: Verify acceptance criteria

- [ ] Manual test: webapp shows items from all 3 sources with correct source indicators
- [ ] Manual test: items appearing in multiple sources show all source badges
- [ ] Manual test: incomplete items are properly indicated
- [ ] Manual test: webapp still works when Jellyfin is not configured
- [ ] Run full test suite (`go test -v ./...`)
- [ ] Run build (`make`)

### Task 7: Update documentation

- [ ] Update CLAUDE.md: document new env vars, new `/api/items` endpoint, unified item concept
- [ ] Move this plan to `docs/plans/completed/`
