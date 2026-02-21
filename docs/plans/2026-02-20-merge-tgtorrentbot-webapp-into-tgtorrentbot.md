# Merge tgtorrentbot-webapp into tgtorrentbot Repository

## Overview
Move webapp source code and static assets into the main bot repo, reorganize both binaries under `cmd/`, unify into a single Go module, create separate named Dockerfiles, and update the Makefile with explicit targets for each service.

## Context
- Files involved:
  - All root-level `package main` bot files: `main.go`, `handler.go`, `update_check.go`, `read_settings.go`, `settings.go`
  - `commands/`, `environment/` packages (unchanged)
  - `Dockerfile` (bot, to rename)
  - `Makefile`, `docker-compose.yaml`, `go.mod`, `go.sum`
  - Webapp files from `../tgtorrentbot-webapp/`: `main.go`, `static/index.html`, `go.mod`
- Related patterns: `cmd/` layout for multi-binary Go repos; `embed.FS` for static assets
- Dependencies: webapp adds no new unique dependencies vs the bot (both share the same external packages; bot adds `github.com/minya/telegram`)

## Development Approach
- No TDD needed - this is a structural reorganization, no new logic
- Complete each task before moving to the next
- Build must pass after each task

## Implementation Steps

### Task 1: Create cmd/ layout for the bot

**Files:**
- Create: `cmd/tgtorrentbot/` directory
- Move: `main.go` → `cmd/tgtorrentbot/main.go`
- Move: `handler.go` → `cmd/tgtorrentbot/handler.go`
- Move: `update_check.go` → `cmd/tgtorrentbot/update_check.go`
- Move: `read_settings.go` → `cmd/tgtorrentbot/read_settings.go`
- Move: `settings.go` → `cmd/tgtorrentbot/settings.go`

- [x] Move all root-level `package main` files to `cmd/tgtorrentbot/`
- [x] Verify `go build ./cmd/tgtorrentbot/` compiles cleanly (imports stay the same since module path hasn't changed)

### Task 2: Add webapp into cmd/tgtorrentbot-webapp/

**Files:**
- Create: `cmd/tgtorrentbot-webapp/main.go` (from `../tgtorrentbot-webapp/main.go`)
- Create: `cmd/tgtorrentbot-webapp/static/index.html` (from `../tgtorrentbot-webapp/static/index.html`)
- Modify: `go.mod` (merge webapp deps if any are missing)

- [x] Copy webapp `main.go` to `cmd/tgtorrentbot-webapp/main.go`; keep package declaration as `package main`
- [x] Copy `static/` directory contents to `cmd/tgtorrentbot-webapp/static/`
- [x] Add `//go:embed static` directive and `embed.FS` variable; update the static file server to use `http.FileServer(http.FS(subFS))` so static assets are baked in at build time
- [x] Check if webapp's `go.mod` has deps not in the bot's `go.mod`; add any missing ones with `go get`
- [x] Verify `go build ./cmd/tgtorrentbot-webapp/` compiles cleanly

### Task 3: Create Dockerfile.bot and Dockerfile.webapp

**Files:**
- Create: `Dockerfile.bot` (based on existing `Dockerfile`, build path updated to `./cmd/tgtorrentbot`)
- Create: `Dockerfile.webapp` (based on webapp's `Dockerfile`, build path updated to `./cmd/tgtorrentbot-webapp`; no static COPY needed since assets are embedded)
- Delete: `Dockerfile` (replaced by `Dockerfile.bot`)

- [ ] Create `Dockerfile.bot` with build command `go build -o /out/tgtorrentbot ./cmd/tgtorrentbot`
- [ ] Create `Dockerfile.webapp` with build command `go build -o /out/tgtorrentbot-webapp ./cmd/tgtorrentbot-webapp`; remove the `COPY --from=build /app/static ./static` line since files are embedded
- [ ] Delete the old root-level `Dockerfile`

### Task 4: Update Makefile

**Files:**
- Modify: `Makefile`

- [ ] Update `binaries` target to `go build -o bin/ ./cmd/...`
- [ ] Rename `image` to `bot-image`, pointing to `Dockerfile.bot`
- [ ] Add `webapp-image` target pointing to `Dockerfile.webapp`
- [ ] Add `images` target that runs `bot-image` and `webapp-image`
- [ ] Keep `.DEFAULT_GOAL := binaries`

### Task 5: Update docker-compose.yaml

**Files:**
- Modify: `docker-compose.yaml`

- [ ] Add `build: { context: ., dockerfile: Dockerfile.bot }` to the `tgtorrentbot` service (optional: keeps compose self-contained)
- [ ] Add `build: { context: ., dockerfile: Dockerfile.webapp }` to the `tgtorrentbot-webapp` service

Note: Image names stay unchanged (`tgtorrentbot_img`, `tgtorrentbot-webapp_img`) so deployment is unaffected.

### Task 6: Verify the build

- [ ] Run `go build ./...` from repo root — should produce no errors
- [ ] Run `go test ./...` — existing test failures are pre-existing and acceptable
- [ ] Run `docker buildx build -f Dockerfile.bot .` — must succeed
- [ ] Run `docker buildx build -f Dockerfile.webapp .` — must succeed

### Task 7: Update documentation

- [ ] Update `CLAUDE.md`: reflect new `cmd/` layout, new Dockerfile names, new Make targets, and note that static assets are embedded
- [ ] Update memory file with new project structure
- [ ] Move this plan to `docs/plans/completed/`
