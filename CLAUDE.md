# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a Telegram bot for managing torrents via a Transmission RPC client. Users can search for torrents on Rutracker, download them to Transmission, list active torrents with pagination, and receive notifications when downloads complete. It also supports an optional Telegram Mini Web App for browsing torrents and an optional MCP server (stdio or HTTP+bearer-token transport) that exposes the media library to Claude Code for tag fixes and library maintenance.

## Build and Run

### Build all binaries
```bash
make
# or directly:
go build -o bin/ ./cmd/...
```

### Build Docker images
```bash
make images          # builds bot, webapp, and mcp images
make bot-image       # builds only the bot image (tgtorrentbot_img)
make webapp-image    # builds only the webapp image (tgtorrentbot-webapp_img)
make mcp-image       # builds only the mcp image (tgtorrentbot-mcp_img)
# or directly:
docker-buildx build --tag tgtorrentbot_img -f Dockerfile.bot .
docker-buildx build --tag tgtorrentbot-webapp_img -f Dockerfile.webapp .
docker-buildx build --tag tgtorrentbot-mcp_img -f Dockerfile.mcp .
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
- `TGT_ALLOWED_USERS` - Comma-separated Telegram user IDs allowed to use the bot and the webapp (e.g. `123456789,987654321`). Settings-file key: `allowedUsers` (JSON array of numbers); startup fails when it is empty

The webapp requires `TGT_BOTTOKEN`, `TGT_DOWNLOADPATH` and `TGT_ALLOWED_USERS` (plus the Transmission/Rutracker variables for the features that use them). The MCP server requires only `TGT_DOWNLOADPATH`.

### Optional environment variables:
- `TGT_LOGLEVEL` - Log level (e.g., "info", "debug")
- `TGT_WEBAPP_URL` - URL for the Telegram Mini Web App (enables web app menu button)
- `TGT_WEBHOOK_SECRET` - Bot only, optional. The `secret_token` registered with `setWebhook`; Telegram sends it in `X-Telegram-Bot-Api-Secret-Token` and the bot rejects webhook requests without it (401). Defaults to a value derived from the bot token, so it only needs setting to rotate it independently. 1-256 chars of `A-Za-z0-9_-`. Settings-file key: `webhookSecret`
- `TGT_JELLYFIN_URL` - Jellyfin server URL (e.g., `http://tgt-jellyfin:8096`); webapp works without it
- `TGT_JELLYFIN_API_KEY` - Jellyfin API key (generated from Jellyfin admin dashboard)
- `TGT_AUDIOBOOKSHELF_URL` - Audiobookshelf server URL (e.g., `http://tgt-audiobookshelf:80`); webapp works without it
- `TGT_AUDIOBOOKSHELF_API_KEY` - Audiobookshelf API key (from Audiobookshelf Settings → Users → select user → API Token)
- `TGT_AUDIOBOOKSHELF_LIBRARY_ID` - MCP server only, optional. Default library for `abs_remove_issues`; when unset, the single `mediaType=book` library is looked up once and cached
- `TGT_INCOMPLETE_PATH` - Path to Transmission's incomplete downloads directory (defaults to `{DownloadPath}/incomplete`)
- `TGT_MCP_TOKEN` - MCP server only. When set, serves MCP over Streamable HTTP on `:8080` behind OAuth 2.1. The token is the shared secret the user types into the `/authorize` login form. Unset → stdio transport
- `TGT_MCP_PUBLIC_URL` - MCP server only. Required when `TGT_MCP_TOKEN` is set. Public HTTPS base URL (e.g. `https://mcp.example.com`); used as OAuth issuer/audience and to build absolute endpoint URLs in `.well-known` metadata
- `TGT_MCP_REDIRECT_HOSTS` - MCP server only, optional. Comma-separated hosts allowed as OAuth redirect targets (e.g. `claude.ai,claude.com`); loopback is always allowed. When set, `/register` and `/authorize` refuse any other host, which blocks consent phishing through self-registered clients. Unset → any https host
- `TGT_MCP_JWT_SECRET` - MCP server only, optional. Extra key material for the JWT signing secret, which is SHA-256 of a label plus `TGT_MCP_TOKEN` when unset and HMAC-SHA256(`TGT_MCP_JWT_SECRET`, label plus `TGT_MCP_TOKEN`) when set (`deriveJWTSecret` in `config.go`). Rotate it to invalidate every outstanding access+refresh token without changing the login secret; rotating `TGT_MCP_TOKEN` invalidates them too

Settings file uses 1Password secret references (e.g., `op://Private/...`).

Both sources go through the same `validateSettings` (`cmd/tgtorrentbot/read_settings.go`): every required value must be set in the settings file too, under its JSON key (`botToken`, `webHookURL`, `downloadPath`, `transmissionRPC.{address,user,password}`, `rutrackerConfig.{username,password}`, `allowedUsers`). An empty `webHookURL` would make `setWebhook` deregister the webhook, so the bot refuses to start instead.

## Repository Layout

This repo uses a `cmd/` layout for multiple binaries:
- `cmd/tgtorrentbot/` — Telegram bot binary (main.go, handler.go, update_check.go, read_settings.go, settings.go)
- `cmd/tgtorrentbot-webapp/` — Web app binary (main.go, payloads.go, websocket.go, init_data.go, static/index.html embedded via go:embed)
- `cmd/tgtorrentbot-mcp/` — MCP server binary for Claude Code integration (main.go, config.go, path.go, list_media.go, tags.go, abs.go, move.go, remove_torrent.go, jellyfin.go; OAuth 2.1: jwt.go, oauth_metadata.go, oauth_register.go, oauth_authorize.go, oauth_token.go)
- `commands/` — Bot command implementations (shared library)
- `environment/` — Shared Env struct
- `internal/` — Internal helpers not exported outside this module
  - `internal/file_id_lookup.go` — `FileIDLookup` used by the file-upload and magnet-link flows
  - `internal/transmissionrpc/` — `New(address, user, password)` builds the Transmission client every binary uses: 30 s timeout, and a transport that handles the `X-Transmission-Session-Id` 409 dance under a mutex, because `odwrtw/transmission` stores the session id in an unsynchronised field and the client is shared across goroutines. Do not call `transmission.New` directly in production code
  - `internal/torrentlabels/` — the Transmission label layout (`[chatID, category, userID]`) shared by the bot and the webapp
  - `internal/category/` — dependency-free download category list (`All()`, `Names()`, `Parse()`, `DisplayName()`), the single source of truth for every binary and the webapp frontend
  - `internal/media/` — Shared media package: `FilesystemScanner`, `JellyfinClient`, `AudiobookshelfClient`, `MergeItems`, `TorrentInfo`, `UnifiedItem`, `FsItem`, `JellyfinItem`, `AudiobookshelfItem`. Used by both the webapp and MCP binaries.
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
- Starts HTTP server on port 80 to receive webhook updates, authenticated by the webhook `secret_token` (`cmd/tgtorrentbot/webhook.go`, served via `telegram.WebhookHandler`)

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
- Polls Transmission every minute for torrent completion, always (from start-up, without pausing), so torrents added from the webapp or outside the bot are observed; a successful bot command triggers an extra immediate check
- Compares current state with previous state
- Sends Telegram notification when torrent completes: PercentDone goes below 1 → 1 between checks, or a torrent not seen before is complete with Transmission `doneDate` at or after the previous poll (catches restarts, webapp adds and torrents that complete between two polls)
- Completions that happen while the bot is down are not notified (the first check counts from process start)
- Sends completion notification only; does not move files between directories
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

Downloads are organized by category (`internal/category/category.go`, the single source of truth; `commands/category.go` re-exports it for the bot):
- `movies` - Фильмы (Films)
- `shows` - Сериалы (TV Shows)
- `music` - Музыка (Music)
- `musicvideos` - Music Videos
- `audiobooks` - Аудиокниги (Audiobooks)
- `others` - Другое (Other)

Categories determine the download subdirectory: `{DownloadPath}/{category}/`

Everything else derives from `internal/category`: the bot's category keyboards, the webapp's `validCategories`, the MCP server's `knownCategories` and `list_media` description, and the webapp frontend, which loads the list from `/categories.js` (served unauthenticated by `cmd/tgtorrentbot-webapp/categories.go`) and builds its category grid cards, `categoryNames`, `counts` and download-modal buttons from it. The virtual `incomplete` category is the one frontend-only entry and is never in the list.

**When adding/removing a category, update these locations:**
1. `internal/category/category.go` — constant, `All()`, `DisplayName()`
2. `docker-compose.yaml` — a `/media/{category}` volume on jellyfin and a `/data/{category}` volume on dufs (`TestComposeMountsCoverAllCategories` in `internal/category` fails when one is missing)
3. `README.md` — the category → directory table

### Torrent Labels Convention

Transmission torrent labels are used to store metadata (layout defined in `internal/torrentlabels`; write them with `torrentlabels.New`, test ownership with `torrentlabels.OwnedBy`):
- `Labels[0]` - chatID (string) - Telegram chat that initiated the download and gets the completion notification
- `Labels[1]` - category (string) - Download category (e.g., "movies", "shows")
- `Labels[2]` - userID (string) - Telegram user who added the torrent, its owner. Differs from the chat ID for a bot download started in a group chat (the chat ID is then the negative group ID). The webapp lists and deletes torrents by owner and writes `[userID, category, userID]`. Torrents labelled before `Labels[2]` existed fall back to `Labels[0]` as owner

Labels are set when adding a torrent (`finalizeTorrent` in `commands/download.go`, `handleDownloadTorrent` in the webapp) and read by:
- Completion checker to notify the correct chat (`update_check.go:144`)
- Completion checker to determine category for move (`update_check.go:156`)
- List command to display category (`list.go`)
- Webapp to show and delete only the user's own torrents (`userTorrents`, `findUserTorrent`)

### Torrent Completion Tracking

The completion checker uses Transmission labels to track which chat initiated each download:
- When adding torrent, chatID and category are stored in labels (`commands/download.go:85`)
- Completion routine reads labels to determine where to send notification
- Compares `PercentDone` between checks to detect transitions to 1, and uses `doneDate` for torrents first seen already complete (`newlyCompleted` in `update_check.go`)
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
7. Sets torrent labels: `[chatID, category, userID]`. If that fails, the torrent is removed again (data kept) and the user is told, as the webapp does; an unlabelled torrent would get no notification and be invisible in the webapp
8. Sends confirmation message. An add Transmission rejects is reported to the user too, a duplicate as "already in Transmission" (`replyAddFailed`)

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

### MCP Server

Optional binary (`cmd/tgtorrentbot-mcp/`) that exposes media-library tools to Claude Code over stdio. Deployed alongside the bot for remote tag fixing ("my cyrillic audiobook tags are mojibake — fix them") without SSH'ing in manually. Current tools:

- `list_media(category?, query?, recursive?)` — reuses `internal/media.MergeItems` to return torrents + filesystem + Jellyfin + Audiobookshelf items with the resolved filesystem path when present: the path the scan actually found (`UnifiedItem.Paths`), i.e. the completed `{DownloadPath}/{category}/{name}` when it exists, else `{IncompletePath}/{name}` for in-progress downloads and orphans of the virtual `incomplete` category (accepted as a `category` filter). Each item carries recency fields: `added_date` (Transmission `addedDate`, torrents only) and `mtime` (newest top-level filesystem mtime, on-disk items only), both RFC 3339 UTC — `torrent_id` is not a recency signal since filesystem-only items have none. `recursive=true` adds a `files` list (path/size/mtime) per item, e.g. individual show episodes; capped at `maxListedFiles` total with `files_truncated` set when hit.
- `read_tags(path, recursive?)` — reads ID3v2 tags from `.mp3` files via `github.com/bogem/id3v2/v2`. Returns title/artist/album/album_artist/composer/year/genre/track/disc/comment. Emits an `encoding_hint: "cp1251"` flag when tags look like cp1251 bytes mis-decoded as Latin-1 (common for rutracker cyrillic downloads), telling Claude when to call `write_tags` with `fix_encoding`.
- `write_tags(path, tags?, fix_encoding?, recursive?)` — two modes (combinable, applied in order fix → explicit): (1) re-decode text frames (T*** and user-defined TXXX), comment frames (COMM) and lyrics frames (USLT) from a codepage (currently only `cp1251`) to Unicode — only values that pass `looksLikeCP1251Mojibake` (mostly U+00C0–U+00FF letters rather than ASCII) are rewritten, so correct UTF-8/Latin-1 text survives; written frames use UTF-8 in ID3v2.4 tags and UTF-16 in v2.3 tags (`unicodeEncoding`; v2.3 has no UTF-8 and strict readers show it as mojibake), and a tag keeps its version; (2) explicit overrides via `tags` map (keys: title, artist, album, album_artist, composer, year, genre, track, disc, comment). For multi-CD layouts where track/disc are missing, the model is expected to read the directory via `read_tags` recursive, decide the right TRCK/TPOS per file from filenames and parent folder names, and send explicit overrides — no path-parsing heuristic lives in the tool. Writes are guarded to `TGT_DOWNLOADPATH` via `resolvePath`, directory scans pick up regular files only (symlinked `.mp3`s are skipped), and files are saved by `saveTag` (random exclusive temp file, fsync, rename; not id3v2's `Tag.Save`, whose fixed `<file>-id3v2` temp name is unsafe); only `.mp3` is supported in this version.
- `abs_rescan` — triggers a scan of every Audiobookshelf library and returns the ids whose scan started. A tool error when ABS isn't configured or any list/scan request fails (connection error, 401/403, 5xx), so a failed rescan is never reported as done.
- `abs_remove_issues(library_id?)` — `DELETE /api/libraries/{id}/issues`: drops every ABS item flagged `isMissing`/`isInvalid`. ABS never removes items whose files moved or vanished — a rescan only flags them — so this is the cleanup step after moving/deleting audiobooks. Library id: param → `TGT_AUDIOBOOKSHELF_LIBRARY_ID` → the single `mediaType=book` library (looked up via `GET /api/libraries`, cached in `AudiobookshelfClient`; errors if there are zero or several). Database entries only; files untouched.
- `abs_remove_item(item_id)` — `DELETE /api/items/{id}` (no `?hard=1`, so files stay on disk) for a single `li_…` item. Both remove tools need an ABS token whose user may delete (403 otherwise).
- `move_media(source, destination, torrent_id?)` — renames/moves a file or directory within `TGT_DOWNLOADPATH`. Source must exist (resolved via `resolvePath`); destination must not (validated via `resolveDestPath`, which allows a not-yet-existing target but still rejects traversal and symlinked-parent escapes); parent dirs are auto-created; existing destinations are never clobbered — the rename itself is atomic no-replace (`renameNoReplace`: `renameat2` `RENAME_NOREPLACE` on Linux, `renameatx_np` `RENAME_EXCL` on macOS, refused elsewhere), so a destination that appears mid-move is not overwritten; cross-device moves are rejected (no copy fallback). A destination inside the source and a destination on another filesystem (device ids of the source and the destination's nearest existing ancestor) are refused before the torrent is untracked, and parent directories created for a move that then fails are removed again. The download root, the category directories and the incomplete directory are refused as source or destination, and so is a source that is itself a symlink (it would move the link's target). When `torrent_id` is passed, the torrent is removed from Transmission with data kept **before** the rename (after path validation, and only when that torrent owns the source: its `{downloadDir}/{name}` or `{IncompletePath}/{name}` equals, contains or lies inside the source, else the move is refused so a wrong id cannot untrack an unrelated torrent), so Transmission never sees files vanish; a torrent id that no longer exists is non-fatal (move proceeds, `torrent_note` explains), any other removal failure aborts before moving. Filesystem-only items omit `torrent_id`. Primary use is fixing TV-show folders Jellyfin mis-identifies (it matches shows by folder name) — the model strips release junk and translates transliterated titles to canonical English (`Proslushka.S01.2002…` → `The Wire (2002)`); no naming heuristic lives in the tool.
- `remove_torrent(torrent_id, delete_data?)` — removes a torrent from Transmission. `delete_data` defaults to **false** (keeps files on disk, only stops tracking) — for moves, prefer passing `torrent_id` to `move_media`, which does this itself. Mirrors `commands/remove_torrent.go`. No-op error when Transmission isn't configured.
- `jellyfin_rescan` — triggers a Jellyfin library refresh via `JellyfinClient.RefreshLibrary()` so renamed folders are re-identified. A tool error when Jellyfin isn't configured or the refresh request fails.

**Fixing a mis-identified TV show:** `list_media(category="shows")` → `move_media` to a clean `Show Name (Year)` folder, passing the item's `torrent_id` if its sources include `"torrent"` → `jellyfin_rescan`. Jellyfin treats the renamed path as a new item and re-matches from the clean name, dropping the stale wrong-id entry on scan.

All tool descriptions are in `main.go:registerTools` — keep them specific enough that Claude can pick the right tool from a natural-language request and knows when to chain calls (e.g., after `write_tags` on audiobooks, always call `abs_rescan`; after `write_tags` on music/musicvideos and after `move_media` on shows, call `jellyfin_rescan`), and state the real limits (only `.mp3`, only `cp1251`) so Claude does not make calls bound to fail.

**Only `TGT_DOWNLOADPATH` is required.** Transmission, Jellyfin, and Audiobookshelf env vars are optional — missing ones just reduce what `list_media` returns and disable `abs_rescan`.

Logs go to **stderr**, not stdout — stdout is reserved for MCP JSON-RPC framing.

**Transports:**

- **stdio** (default): for local Claude Code. Spawn via SSH from `~/.claude.json`:
  ```json
  {
    "mcpServers": {
      "tgtorrentbot": {
        "command": "ssh",
        "args": ["user@host", "/opt/tgtorrentbot/mcp-wrapper.sh"]
      }
    }
  }
  ```
  The wrapper script on the server exports env vars and `exec`s the binary.

- **HTTP** (streamable, OAuth 2.1): set `TGT_MCP_TOKEN` + `TGT_MCP_PUBLIC_URL` to enable. The binary listens on `:8080` and serves:
  - `GET /.well-known/oauth-authorization-server` (RFC 8414) and `GET /.well-known/oauth-protected-resource` (RFC 9728) — metadata for Claude's connector to discover endpoints
  - `POST /register` — Dynamic Client Registration (RFC 7591). No auth; body capped at 16 KiB, at most 10 redirect URIs of up to 2 KB each and a `client_name` of up to 200 bytes; returns a `client_id` that is itself an HS256 JWT encoding the submitted `redirect_uris` and `client_name`. The server has no client database — every `/authorize` request re-decodes the `client_id` to recover the registered metadata.
  - `GET /authorize` — renders an HTML login form that shows the host the code will be sent to and cannot be framed (`X-Frame-Options: DENY`, `frame-ancestors 'none'`); `POST /authorize` validates the typed secret against `TGT_MCP_TOKEN` (constant-time), issues a 60-second in-memory authorization code, redirects back to the client with `code`+`state`. PKCE S256 mandatory. Login attempts are rate-limited per client IP (`CF-Connecting-IP`, else the TCP peer; never `X-Forwarded-For`, which the client controls) and failed logins are capped globally across all clients, so rotating IPs or headers buys no extra guesses (during an attack the owner can be locked out for up to a minute).
  - `POST /token` — supports `grant_type=authorization_code` (verifies code + PKCE verifier) and `grant_type=refresh_token`. Issues an access JWT (1h) and a refresh JWT that expires 30 days after the login (the `auth_time` claim, carried unchanged through every refresh, so a refresh token cannot renew itself forever; the refresh grant also checks `iss`/`aud`), both signed with a key derived from `TGT_MCP_TOKEN` and, when set, `TGT_MCP_JWT_SECRET` (`deriveJWTSecret`).
  - `POST /` — the MCP handler, wrapped in `jwtAuth`: parses `Authorization: Bearer <jwt>`, verifies HS256 signature + `exp` + `iss` + `aud` + `typ=access`. On failure returns 401 with `WWW-Authenticate: Bearer resource_metadata="..."` so clients re-discover after secret rotation. Mounted at `/` (not `/mcp`) because the protected-resource metadata advertises the base URL as the resource, and Claude's connector posts MCP to exactly that URL. More specific OAuth routes (`/authorize`, `/token`, `/register`, `/.well-known/*`) win via Go's longest-prefix mux matching.

  **Stateless by design:** no database, no volume, no client persistence. `TGT_MCP_TOKEN` is the human-typed shared secret on the login form; The JWT signing key always derives from the token, mixed with `TGT_MCP_JWT_SECRET` when that is set. Rotating either env var invalidates every outstanding token — the only revocation mechanism available. The single piece of server state is the in-memory 60-second code map, which is fine to lose on restart (clients just redo the redirect).

  The containerized `tgt-mcp` service listens on `:8080` inside `tunnel-net`; `cloudflared` exposes it publicly (hostname configured in Cloudflare Zero Trust). Point your claude.ai connector at the public URL — the connector discovers OAuth endpoints via `.well-known`, runs DCR, redirects you to the login form, and stores the resulting tokens on its side.

  Secrets come from env (`.env` or compose secret); do not hard-code.

### Telegram Mini Web App
- Optional feature enabled by setting `WebAppURL` (`TGT_WEBAPP_URL` env var)
- On startup, `cmd/tgtorrentbot/main.go` configures the Telegram chat menu button to open the web app
- The web app binary lives in `cmd/tgtorrentbot-webapp/`; its static assets (`static/index.html`) are embedded at build time via `//go:embed static` — no separate static file COPY step needed
- The web app is a separate service (`tgtorrentbot-webapp`) defined in `docker-compose.yaml`, built with `Dockerfile.webapp`
- Uses the same Rutracker/Transmission credentials
- The web app button also appears in the `/list` command keyboard
- API endpoints (registered in `cmd/tgtorrentbot-webapp/main.go`). The HTTP ones require the `X-Telegram-Init-Data` header with valid Telegram HMAC from a user in `TGT_ALLOWED_USERS` (`makeHandler`); a wrong method gets 405:
  - `GET /api/items` — returns unified media items merged from Transmission, filesystem, Jellyfin, and Audiobookshelf (see Unified Media Items below)
  - `DELETE /api/items/{id}` — "Remove data": removes the user's torrent for the item, if any (data kept by Transmission), then deletes its data from `{DownloadPath}/{category}/{name}` and `{IncompletePath}/{name}`. `{id}` is `base64url("{category}:{name}")`; the virtual `incomplete` category maps to the incomplete dir only. Filesystem items are shared between allowed users (`/api/items` lists every user's on-disk data), so any of them may delete filesystem-only data, but data that another user's torrent still uses (same name and category, `findOtherUsersTorrent`) is refused with 403
  - `DELETE /api/items/{id}/torrent` — "Remove torrent": removes only the user's torrent, files stay
  - `GET /api/torrents` — lists the requesting user's torrents (by owner label) with category, sorted by ID descending
  - `POST /api/torrents/download` — downloads from Rutracker and adds to Transmission; body: `{"downloadUrl":"...","category":"..."}`
  - `GET /api/search?q=<query>` — searches Rutracker, returns up to 20 results sorted by seeders
  - `GET /api/ws` — WebSocket of live torrent progress for the user. Browsers cannot set headers on a WebSocket, so the init data travels as the subprotocol `tg-auth.<base64url(initData)>` (`websocket.go`)
  - `GET /categories.js` — unauthenticated, the category list for the frontend (see Category System)

### Unified Media Items

The webapp displays a unified view of media items merged from four sources:
- **Transmission torrents** — active/completed downloads
- **Filesystem** — directories in `{downloadPath}/{category}/` and the incomplete directory
- **Jellyfin** — library items from the Jellyfin media server (optional)
- **Audiobookshelf** — audiobook library items from Audiobookshelf (optional)

Jellyfin and Audiobookshelf are optional: `/api/items` fetches them concurrently and lists without whichever has not answered within `optionalSourceTimeout` (8 s), and `DELETE /api/items/{id}` triggers their library refreshes in a goroutine after responding, so a hung Jellyfin/ABS never fails a listing or a deletion.

Items are matched by normalized name (case-insensitive, trimmed) + category and merged into a single `UnifiedItem`. Each item shows which sources it appears in via a `sources` array (e.g., `["torrent", "filesystem", "jellyfin", "audiobookshelf"]`). Items found in the incomplete directory get `isIncomplete: true`.

An incomplete-directory folder that matches an existing torrent or completed `{category}/` directory (by normalized name) merges into that entry, keeping its real category with the `isIncomplete` flag set. A folder that matches **nothing** — orphaned leftover data, e.g. a stalled download whose torrent was removed — is surfaced under the **virtual `incomplete` category** (`media.IncompleteCategory`, see `internal/media/unified.go`). This is a cleanup affordance, not a real download category: nothing can be downloaded into it (it is absent from `validCategories`), the frontend only renders its grid card when non-empty, and `DELETE /api/items/{id}` maps it to `{IncompletePath}/{name}` instead of `{DownloadPath}/{category}/{name}` so the leftover data can be removed.

Audiobookshelf items are matched against existing entries by path prefix: if Audiobookshelf returns sub-items (e.g., `Author - Book/Том 1`, `Author - Book/Том 2`), they merge into the parent filesystem entry (`Author - Book`) rather than creating separate items.

Key files:
- `cmd/tgtorrentbot-webapp/unified.go` — merge logic
- `cmd/tgtorrentbot-webapp/filesystem.go` — scans download directories
- `cmd/tgtorrentbot-webapp/jellyfin.go` — Jellyfin API client
- `cmd/tgtorrentbot-webapp/audiobookshelf.go` — Audiobookshelf API client
- `cmd/tgtorrentbot-webapp/payloads.go` — response types including `UnifiedItem`

The frontend (`static/index.html`) fetches from `/api/items` and shows source badges (T/F/J/A) on each item. Items with a torrent or filesystem source get a ⋮ menu: "Remove torrent" only when the item has a torrent source, "Remove data" for both (so filesystem-only items can be deleted too).

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
- `github.com/modelcontextprotocol/go-sdk` - Official MCP SDK, used by `cmd/tgtorrentbot-mcp` (stdio transport, typed `AddTool` with JSON-schema-tagged input structs)
- `github.com/bogem/id3v2/v2` - ID3v2 tag reader/writer for `.mp3` files (used by `read_tags`/`write_tags` MCP tools)
- `golang.org/x/text/encoding/charmap` - `Windows1251` decoder used by `fix_encoding="cp1251"` in `write_tags`

## Notes

- UI text is in Russian (search for `// TODO: translate` comments)
- Completion checker polls every 1 minute
- Search returns max 10 results sorted by seeders
- Plain text messages without slash are treated as search queries, unless they start with `magnet:?` (routed to magnet download)
