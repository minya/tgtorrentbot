# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a Telegram bot for managing torrents via a Transmission RPC client. Users can search for torrents on Rutracker, download them to Transmission, list active torrents with pagination, and receive notifications when downloads complete. It also supports an optional Telegram Mini Web App for browsing torrents.

## Build and Run

### Build all binaries
```bash
make
# or directly:
go build -o bin/ ./cmd/...
```

### Build Docker images
```bash
make images          # builds both bot and webapp images
make bot-image       # builds only the bot image (tgtorrentbot_img)
make webapp-image    # builds only the webapp image (tgtorrentbot-webapp_img)
# or directly:
docker-buildx build --tag tgtorrentbot_img -f Dockerfile.bot .
docker-buildx build --tag tgtorrentbot-webapp_img -f Dockerfile.webapp .
```

### Run locally
```bash
# Requires settings.json or environment variables
go run ./cmd/tgtorrentbot -settings settings.json -log-level info
```

### Run single test file
```bash
go test -v ./commands -run TestSpecificFunction
```

### Run all tests
```bash
go test -v ./...
```

## Configuration

The bot can be configured via environment variables or a JSON settings file. Settings are read in priority order:
1. Environment variables (see `read_settings.go:28`)
2. Settings file (default: `settings.json`)

### Required environment variables:
- `TGT_BOTTOKEN` - Telegram bot token
- `TGT_WEBHOOKURL` - Webhook URL for Telegram updates
- `TGT_DOWNLOADPATH` - Path for downloads
- `TGT_RPC_ADDR` - Transmission RPC address
- `TGT_RPC_USER` - Transmission RPC user
- `TGT_RPC_PASSWORD` - Transmission RPC password
- `TGT_RUTRACKER_USERNAME` - Rutracker username
- `TGT_RUTRACKER_PASSWORD` - Rutracker password

### Optional environment variables:
- `TGT_LOGLEVEL` - Log level (e.g., "info", "debug")
- `TGT_WEBAPP_URL` - URL for the Telegram Mini Web App (enables web app menu button)
- `TGT_JELLYFIN_URL` - Jellyfin server URL (e.g., `http://tgt-jellyfin:8096`); webapp works without it
- `TGT_JELLYFIN_API_KEY` - Jellyfin API key (generated from Jellyfin admin dashboard)
- `TGT_INCOMPLETE_PATH` - Path to Transmission's incomplete downloads directory (defaults to `{DownloadPath}/incomplete`)

Settings file uses 1Password secret references (e.g., `op://Private/...`).

## Repository Layout

This repo uses a `cmd/` layout for multiple binaries:
- `cmd/tgtorrentbot/` — Telegram bot binary (main.go, handler.go, update_check.go, read_settings.go, settings.go)
- `cmd/tgtorrentbot-webapp/` — Web app binary (main.go, filesystem.go, jellyfin.go, unified.go, payloads.go, static/index.html embedded via go:embed)
- `commands/` — Bot command implementations (shared library)
- `environment/` — Shared Env struct
- `internal/` — Internal helpers not exported outside this module (e.g., `FileIDLookup`)
- `Dockerfile.bot` — Docker build for the bot
- `Dockerfile.webapp` — Docker build for the web app (static assets are embedded, no separate COPY step)

## Architecture

### Core Components

**cmd/tgtorrentbot/main.go** - Entry point that:
- Initializes logger with configurable level and pretty printing
- Loads settings (env vars or file)
- Creates Transmission client
- Sets up Telegram API with webhook
- Configures Telegram chat menu button for web app (if `WebAppURL` is set)
- Starts completion check routine (monitors torrent progress)
- Starts HTTP server on port 80 to receive webhook updates

**cmd/tgtorrentbot/handler.go** - `UpdatesHandler` processes incoming Telegram updates:
- Routes messages/callbacks to appropriate command factories
- Each factory implements `Accepts(upd *telegram.Update) (bool, Command)`
- Commands implement `Handle(upd *telegram.Update) error`
- Falls back to error message if no command matches

**environment/environment.go** - `Env` struct provides shared dependencies:
- `TransmissionClient` - RPC client for torrent operations
- `TgApi` - Telegram bot API
- `DownloadPath` - Local download directory
- `RutrackerConfig` - Credentials for Rutracker
- `WebAppURL` - Optional URL for Telegram Mini Web App
- `AllowedUsers` - Whitelist of Telegram user IDs permitted to use the bot
- `FileIDLookup` - Pointer to shared `internal.FileIDLookup` used by the file-upload and magnet-link flows to map long strings to short UUIDs for Telegram callback data (see Download-by-File Flow, Download-by-Magnet Flow)

**cmd/tgtorrentbot/update_check.go** - Background routine that:
- Polls Transmission every minute for torrent completion
- Compares current state with previous state
- Sends Telegram notification when torrent completes (PercentDone 0→1)
- Sends completion notification only; does not move files between directories
- Pauses when all torrents are complete
- Uses chatID stored in torrent labels to notify correct user

### Command Pattern

All bot commands follow the factory pattern in `commands/`:

1. **CommandFactory** interface (`commands/command.go:7`):
   - `Accepts(upd *telegram.Update) (bool, Command)` - checks if update matches this command

2. **Command** interface (`commands/command.go:11`):
   - `Handle(upd *telegram.Update) error` - executes the command logic

3. Each command has:
   - Factory struct (holds `environment.Env`)
   - Command struct (holds parsed params + `environment.Env`)

Registered command factories (`cmd/tgtorrentbot/handler.go:20`):
- `ListCommandFactory` - `/list` shows active torrents (paginated, 5 per page)
- `ListPageCommandFactory` - `/list_page <page>` handles pagination callbacks
- `RemoveTorrentCommandFactory` - `/remove <id>` removes a torrent
- `SearchCommandFactory` - `/search <query>` or plain text searches Rutracker
- `DownloadByMagnetCommandFactory` - detects `magnet:?` URIs in plain text messages, prompts for category
- `DownloadWithCategoryCommandFactory` - `/dlcat <category> <url>` downloads with category (callback only)
- `DownloadFileWithCategoryCommandFactory` - handles file upload with category selection (callback only)
- `DownloadMagnetWithCategoryCommandFactory` - `/dlmagnet <category> <uuid>` downloads magnet with category (callback only)
- `DownloadCommandFactory` - `/dl <url>` prompts for category selection (callback only)
- `DownloadByFileCommandFactory` - handles torrent file uploads, prompts for category

**Command ordering matters:** Commands are matched in order. More specific patterns (e.g., `/dlcat`) must come before general ones (e.g., `/dl`) to avoid incorrect matching. `DownloadByMagnetCommandFactory` must come before `SearchCommandFactory` so magnet URIs aren't treated as search queries.

### Rutracker Integration

The rutracker package has been extracted to an external library: `github.com/minya/rutracker`.

Key functions:
- `NewAuthenticatedRutrackerClient()` - creates client with cookie-based auth
- `authenticate()` - logs in via form POST to get session cookie
- `Find(pattern)` - searches Rutracker, returns parsed results
- `DownloadTorrent(url)` - fetches .torrent file as bytes

The parser uses regex to extract topic ID, URL, title, size, and seeders from Rutracker search results HTML.

### Category System

Downloads are organized by category (`commands/category.go`):
- `movies` - Фильмы (Films)
- `shows` - Сериалы (TV Shows)
- `music` - Музыка (Music)
- `musicvideos` - Music Videos
- `audiobooks` - Аудиокниги (Audiobooks)
- `others` - Другое (Other)

Categories determine the download subdirectory: `{DownloadPath}/{category}/`

**When adding/removing a category, update ALL of these locations:**
1. `commands/category.go` — constant, `AllCategories()`, `DisplayName()`
2. `cmd/tgtorrentbot-webapp/main.go` — `validCategories` slice
3. `cmd/tgtorrentbot-webapp/static/index.html` — `categoryNames` JS object, `counts` JS object, category grid cards, category modal buttons
4. `docker-compose.yaml` — jellyfin volumes (each category needs a `/media/{category}` mount)

### Torrent Labels Convention

Transmission torrent labels are used to store metadata:
- `Labels[0]` - chatID (string) - Telegram chat that initiated the download
- `Labels[1]` - category (string) - Download category (e.g., "movies", "shows")

Labels are set when adding a torrent (`commands/download.go:85-95`) and read by:
- Completion checker to notify the correct chat (`update_check.go:144`)
- Completion checker to determine category for move (`update_check.go:156`)
- List command to display category (`list.go`)

### Torrent Completion Tracking

The completion checker uses Transmission labels to track which chat initiated each download:
- When adding torrent, chatID and category are stored in labels (`commands/download.go:85`)
- Completion routine reads labels to determine where to send notification
- Compares `PercentDone` between checks to detect 0→1 transitions
- Sends Telegram notification to the chat stored in label[0]

## Key Implementation Details

### Search Flow
1. User sends text (plain or `/search <query>`)
2. `SearchCommand` authenticates with Rutracker
3. Results are parsed, sorted by seeders (descending)
4. Top 10 results sent as messages with "Добавить" inline button
5. Button triggers `/dl <url>` callback

### Download Flow
1. User clicks "Добавить" button → triggers `/dl <url>` callback
2. `DownloadCommand` presents category selection keyboard
3. User selects category → triggers `/dlcat <category> <url>` callback
4. `DownloadWithCategoryCommand` authenticates with Rutracker
5. Downloads .torrent file as bytes
6. Base64-encodes and sends to Transmission RPC with download dir `{DownloadPath}/{category}/`
7. Sets torrent labels: `[chatID, category]`
8. Sends confirmation message

### Download-by-File Flow
1. User uploads a `.torrent` file directly to the bot
2. `DownloadByFileCommand` registers the Telegram `file_id` in `internal.FileIDLookup` and receives a short UUID key
3. Category selection keyboard is sent with callbacks of the form `/dlfilecat <category> <uuid>`
4. User picks a category → `DownloadFileWithCategoryCommand.Accepts` resolves the UUID back to the real `file_id`; if the UUID is missing (bot restart), it returns an `expiredFileButtonCommand` that tells the user to re-upload
5. The real file is fetched via `GetFile`/`DownloadFile` and handed to `DownloadCommand.addTorrentAndReply`, which sets labels `[chatID, category]` and adds it to Transmission

**Why the UUID indirection:** Telegram `callback_data` is capped at 64 bytes. Telegram `file_id` values alone are routinely 60+ chars, so embedding the raw `file_id` plus a category prefix overflows the limit and the button silently does nothing. `FileIDLookup` keeps the `file_id` in process memory and puts only a 36-char UUID in the callback. It is a `sync.Mutex`-guarded map used concurrently from webhook handlers, and `Add` bounds memory by evicting entries older than 1 hour.

### Download-by-Magnet Flow
1. User pastes a `magnet:?xt=urn:btih:…` URI into the bot chat (case-insensitive match on `magnet:?` prefix)
2. `DownloadByMagnetCommand` stores the magnet URI in `FileIDLookup` and receives a short UUID key (same indirection as the file-upload flow — magnet URIs exceed the 64-byte callback data limit)
3. Category selection keyboard is sent with callbacks of the form `/dlmagnet <category> <uuid>`
4. User picks a category → `DownloadMagnetWithCategoryCommand.Accepts` resolves the UUID back to the magnet URI; if the UUID is missing (bot restart), it returns an `expiredFileButtonCommand` that tells the user to re-send the link
5. The magnet URI is passed to Transmission via `AddTorrentArg{Filename: magnetURI}` (Transmission RPC accepts magnet URIs in the `filename` field); labels `[chatID, category]` are set via `DownloadCommand.addMagnetAndReply`
6. Transmission resolves torrent metadata asynchronously over DHT/peers

### List with Pagination
1. User sends `/list`
2. Torrents are fetched from Transmission and sorted by ID descending (most recent first)
3. Results are paginated (5 per page) with navigation buttons ("← Назад" / "Вперёд →")
4. Page header shows "Страница X/Y (всего: Z)"
5. If `WebAppURL` is configured, an "Открыть приложение" button links to the web app
6. Pagination callbacks use `/list_page <page_number>`

### Telegram Mini Web App
- Optional feature enabled by setting `WebAppURL` (`TGT_WEBAPP_URL` env var)
- On startup, `cmd/tgtorrentbot/main.go` configures the Telegram chat menu button to open the web app
- The web app binary lives in `cmd/tgtorrentbot-webapp/`; its static assets (`static/index.html`) are embedded at build time via `//go:embed static` — no separate static file COPY step needed
- The web app is a separate service (`tgtorrentbot-webapp`) defined in `docker-compose.yaml`, built with `Dockerfile.webapp`
- Uses the same Rutracker/Transmission credentials
- The web app button also appears in the `/list` command keyboard
- API endpoints (all require `X-Telegram-Init-Data` header with valid Telegram HMAC):
  - `GET /api/items` — returns unified media items merged from Transmission, filesystem, and Jellyfin (see Unified Media Items below)
  - `GET /api/torrents` — lists all torrents with category, sorted by ID descending
  - `POST /api/torrents/remove?id=<n>` — removes a torrent by Transmission ID
  - `POST /api/torrents/download` — downloads from Rutracker and adds to Transmission; body: `{"downloadUrl":"...","category":"..."}`
  - `GET /api/search?q=<query>` — searches Rutracker, returns up to 20 results sorted by seeders

### Unified Media Items

The webapp displays a unified view of media items merged from three sources:
- **Transmission torrents** — active/completed downloads
- **Filesystem** — directories in `{downloadPath}/{category}/` and the incomplete directory
- **Jellyfin** — library items from the Jellyfin media server (optional)

Items are matched by normalized name (case-insensitive, trimmed) + category and merged into a single `UnifiedItem`. Each item shows which sources it appears in via a `sources` array (e.g., `["torrent", "filesystem", "jellyfin"]`). Items found only in the incomplete directory get `isIncomplete: true`.

Key files:
- `cmd/tgtorrentbot-webapp/unified.go` — merge logic
- `cmd/tgtorrentbot-webapp/filesystem.go` — scans download directories
- `cmd/tgtorrentbot-webapp/jellyfin.go` — Jellyfin API client
- `cmd/tgtorrentbot-webapp/payloads.go` — response types including `UnifiedItem`

The frontend (`static/index.html`) fetches from `/api/items` and shows source badges (T/F/J) on each item. Remove button only appears for items with a torrent source.

### Dependency Injection
- `environment.Env` is created once in `cmd/tgtorrentbot/main.go`
- Passed to all command factories in `cmd/tgtorrentbot/handler.go`
- Each command instance gets a copy when created in factory's `Accepts()`

## Dependencies

Key external packages:
- `github.com/minya/telegram` - Custom Telegram bot API client
- `github.com/odwrtw/transmission` - Transmission RPC client
- `github.com/minya/rutracker` - Rutracker search and download client (extracted from this repo)
- `github.com/minya/logger` - Custom logging wrapper (uses zerolog)
- `github.com/minya/goutils` - HTTP utilities (cookie jar, transport)

## Notes

- UI text is in Russian (search for `// TODO: translate` comments)
- Completion checker polls every 1 minute when active
- Search returns max 10 results sorted by seeders
- Plain text messages without slash are treated as search queries, unless they start with `magnet:?` (routed to magnet download)
