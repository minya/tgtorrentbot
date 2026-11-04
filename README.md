# tgtorrentbot — a Telegram bot to download torrents and serve media

A Telegram bot for managing torrents via [Transmission](https://transmissionbt.com/). Users can search [Rutracker](https://rutracker.org/), download torrents, monitor progress, and receive completion notifications. Ships with an optional sidecar [Telegram Mini App](https://core.telegram.org/bots/webapps) that opens directly inside Telegram as a chat menu button.

## Features

- **Search** Rutracker from Telegram (text message or `/search <query>`)
- **Download** torrents directly into Transmission, organized by category
- **List** torrents with pagination (`/list`)
- **Remove** torrents (`/remove <id>`)
- **Completion notifications** — bot messages you when a download finishes
- **Telegram Mini App** — optional sidecar service that opens as a chat menu button inside Telegram; provides a full UI for searching, downloading, and managing torrents without leaving the app
- **Unified media view** — the Mini App shows media items merged from Transmission, the filesystem, Jellyfin and Audiobookshelf with source indicators (T/F/J/A); works without Jellyfin and Audiobookshelf
- **MCP server** — optional binary that exposes the media library to Claude (Claude Code over stdio, or the claude.ai connector over HTTP with OAuth) for fixing audiobook tags, renaming mis-identified shows and library cleanup

## Architecture

```
cmd/tgtorrentbot/        — Telegram bot binary
cmd/tgtorrentbot-webapp/ — Telegram Mini App sidecar binary (static assets embedded via go:embed)
cmd/tgtorrentbot-mcp/    — MCP server binary for Claude (media library tools)
commands/                — Bot command implementations
environment/             — Shared Env struct (dependencies)
internal/                — Shared internals: categories, torrent labels, media merging
                           (Transmission, filesystem, Jellyfin, Audiobookshelf), the
                           Transmission client, graceful shutdown
```

[CLAUDE.md](CLAUDE.md) has the detailed architecture, every MCP tool and the OAuth flow.

The bot uses a webhook-based update flow. Downloads are organized into category subdirectories under the configured download path. Transmission torrent labels store the originating chat ID and category for completion tracking.

## Bot Commands

| Command | Description |
|---|---|
| `<text>` | Search Rutracker for the given text |
| `/search <query>` | Same as plain text search |
| `/list` | List all torrents in Transmission, paginated (5 per page) |
| `/remove <id>` | Remove a torrent and delete its local data |

Downloading is initiated via inline keyboard buttons in search results.

## Download Categories

Downloads are sorted into subdirectories by category:

| Category | Directory |
|---|---|
| Movies | `{downloadPath}/movies/` |
| TV Shows | `{downloadPath}/shows/` |
| Music | `{downloadPath}/music/` |
| Music Videos | `{downloadPath}/musicvideos/` |
| Audiobooks | `{downloadPath}/audiobooks/` |
| Other | `{downloadPath}/others/` |

## Telegram Mini App

The Mini App is a sidecar service that runs alongside the bot and is surfaced as a [chat menu button](https://core.telegram.org/bots/webapps#launching-mini-apps-from-the-menu-button) inside Telegram. When `TGT_WEBAPP_URL` is set, the bot registers the URL as the menu button target on startup. The Mini App also appears as a button in `/list` output.

Authentication uses the standard Telegram Mini App init data mechanism: the client passes `window.Telegram.WebApp.initData` in the `X-Telegram-Init-Data` header, and the server validates the HMAC signature with the bot token.

### Mini App API

All endpoints require `X-Telegram-Init-Data` header with valid Telegram HMAC:

| Method | Endpoint | Description |
|---|---|---|
| GET | `/api/items` | Unified media items merged from Transmission, the filesystem, Jellyfin and Audiobookshelf |
| DELETE | `/api/items/{id}` | Remove data: the item's torrents (if any) and its files |
| DELETE | `/api/items/{id}/torrent` | Remove only the item's torrent; files stay |
| GET | `/api/torrents` | List every torrent (downloads are shared between allowed users), sorted by ID desc |
| POST | `/api/torrents/download` | Add a torrent; body: `{"downloadUrl":"...","category":"..."}` |
| GET | `/api/search?q=<query>` | Search Rutracker, returns up to 20 results |
| GET | `/api/ws` | WebSocket of live torrent progress (init data sent as the `tg-auth.<base64url>` subprotocol) |

## Configuration

Settings are loaded from environment variables or a JSON file (`settings.json` by default), never a mix: if any required `TGT_*` variable is set, the environment is used exclusively and must be complete (a missing variable is a startup error); otherwise the file is used. The file is read as plain JSON, so 1Password `op://` references in it must be resolved first (`op inject -i settings.json -o settings.resolved.json`); unresolved ones are refused at startup.

### Environment Variables

| Variable | Required | Description |
|---|---|---|
| `TGT_BOTTOKEN` | Yes | Telegram bot token |
| `TGT_WEBHOOKURL` | Yes | Webhook URL for Telegram updates |
| `TGT_DOWNLOADPATH` | Yes | Base path for downloads |
| `TGT_RPC_ADDR` | Yes | Transmission RPC address |
| `TGT_RPC_USER` | Yes | Transmission RPC user |
| `TGT_RPC_PASSWORD` | Yes | Transmission RPC password |
| `TGT_RUTRACKER_USERNAME` | Yes | Rutracker username |
| `TGT_RUTRACKER_PASSWORD` | Yes | Rutracker password |
| `TGT_ALLOWED_USERS` | Yes | Comma-separated Telegram user IDs allowed to use the bot and the webapp |
| `TGT_LOGLEVEL` | No | Log level (`info`, `debug`, etc.) |
| `TGT_WEBAPP_URL` | No | Mini App URL; registers it as the Telegram chat menu button |
| `TGT_JELLYFIN_URL` | No | Jellyfin server URL (e.g., `http://tgt-jellyfin:8096`); webapp works without it |
| `TGT_JELLYFIN_API_KEY` | No | Jellyfin API key (generated from Jellyfin admin dashboard) |
| `TGT_AUDIOBOOKSHELF_URL` | No | Audiobookshelf server URL (e.g., `http://tgt-audiobookshelf:80`); webapp and MCP work without it |
| `TGT_AUDIOBOOKSHELF_API_KEY` | No | Audiobookshelf API token (Settings → Users → user → API Token) |
| `TGT_INCOMPLETE_PATH` | No | Path to Transmission's incomplete downloads directory; defaults to `{downloadPath}/incomplete` |
| `TGT_WEBHOOK_SECRET` | No | Bot only. Webhook `secret_token`; defaults to a value derived from the bot token |

The bot needs every required variable. The webapp needs `TGT_BOTTOKEN`, `TGT_DOWNLOADPATH` and `TGT_ALLOWED_USERS` (plus the Transmission and Rutracker variables for the features that use them).

#### MCP server

The MCP server needs only `TGT_DOWNLOADPATH`; Transmission, Jellyfin and Audiobookshelf variables are optional and enable the matching tools.

| Variable | Required | Description |
|---|---|---|
| `TGT_MCP_TOKEN` | No | When set, serve MCP over HTTP on `:8080` behind OAuth 2.1; it is the secret typed into the login form. Unset → stdio |
| `TGT_MCP_PUBLIC_URL` | With `TGT_MCP_TOKEN` | Public HTTPS base URL, used as OAuth issuer and audience |
| `TGT_MCP_REDIRECT_HOSTS` | No | Comma-separated hosts allowed as OAuth redirect targets (e.g. `claude.ai,claude.com`) |
| `TGT_MCP_JWT_SECRET` | No | Extra key material for token signing; rotate it to revoke every token |
| `TGT_AUDIOBOOKSHELF_LIBRARY_ID` | No | Default library for `abs_remove_issues` |

### Settings File (`settings.json`)

```json
{
  "botToken": "...",
  "webHookURL": "https://yourdomain.com/webhook",
  "downloadPath": "/downloads",
  "transmissionRPC": {
    "address": "http://localhost:9091/transmission/rpc",
    "user": "user",
    "password": "password"
  },
  "rutrackerConfig": {
    "username": "...",
    "password": "..."
  },
  "logLevel": "info",
  "webAppURL": "https://yourdomain.com/webapp",
  "allowedUsers": [123456789]
}
```

## Build

### Prerequisites

- Go 1.25+
- Docker (for containerized deployment)

### Build binaries

```bash
make
# or:
go build -o bin/ ./cmd/...
```

### Build Docker images

```bash
make images          # all three images
make bot-image       # tgtorrentbot_img only
make webapp-image    # tgtorrentbot-webapp_img only
make mcp-image       # tgtorrentbot-mcp_img only
```

## Running

### Locally

```bash
op inject -i settings.json -o settings.resolved.json   # if it holds op:// references
go run ./cmd/tgtorrentbot -settings settings.resolved.json -log-level info
```

> **Note:** The bot listens on port 80. On Linux/macOS this requires either running as root, granting `CAP_NET_BIND_SERVICE`, or using a port forwarder (e.g. `sudo sysctl net.ipv4.ip_unprivileged_port_start=80`).

### Docker Compose

Set the required variables in your environment (e.g. via a `.env` file loaded by your shell or Docker tooling), then:

```bash
docker compose up -d
```

The compose stack includes:
- `tgt-bot` — Telegram bot
- `tgt-webapp` — Telegram Mini App sidecar
- `tgt-mcp` — MCP server for Claude (HTTP + OAuth, needs `MCP_TOKEN` and `MCP_PUBLIC_URL`)
- `tgt-transmission` — Transmission BitTorrent client
- `tgt-jellyfin` — Jellyfin media server
- `tgt-audiobookshelf` — Audiobookshelf audiobook server
- `tgt-dufs` — dufs file server over the download directories
- `tgt-cloudflare-tunnel` — Cloudflare tunnel for public access

Compose environment variables:

| Variable | Required | Description |
|---|---|---|
| `BOT_TOKEN` | Yes | Telegram bot token |
| `ALLOWED_USERS` | Yes | Comma-separated Telegram user IDs allowed to use the bot and the webapp |
| `WEBHOOKURL` | Yes | Public webhook URL |
| `PASSWD` | Yes | Transmission RPC password |
| `RUTRACKER_USERNAME` | Yes | Rutracker username |
| `RUTRACKER_PASSWORD` | Yes | Rutracker password |
| `TUNNEL_TOKEN` | Yes | Cloudflare tunnel token |
| `WEBAPP_URL` | No | Mini App public URL; enables the chat menu button and Mini App sidecar |
| `JELLYFIN_API_KEY` | No | Jellyfin API key; enables unified media items with Jellyfin source |
| `AUDIOBOOKSHELF_API_KEY` | No | Audiobookshelf API token; enables the Audiobookshelf source and the MCP `abs_*` tools |
| `MCP_TOKEN` | For `tgt-mcp` | MCP login secret; without it the `tgt-mcp` container exits with an error |
| `MCP_PUBLIC_URL` | For `tgt-mcp` | Public HTTPS URL of the MCP server |
| `DUFS_USER` | For `tgt-dufs` | dufs login user |
| `DUFS_PASSWORD` | For `tgt-dufs` | dufs login password |
| `LOGLEVEL` | No | Log level for the bot, webapp and MCP server (`info`, `debug`, …) |

## Testing

```bash
go test -v ./...
# Run a specific test:
go test -v ./commands -run TestFunctionName
```

## Dependencies

- [`github.com/minya/telegram`](https://github.com/minya/telegram) — Telegram bot API client
- [`github.com/minya/rutracker`](https://github.com/minya/rutracker) — Rutracker search and download client
- [`github.com/odwrtw/transmission`](https://github.com/odwrtw/transmission) — Transmission RPC client
- [`github.com/minya/logger`](https://github.com/minya/logger) — Logging wrapper (zerolog)
- [`github.com/minya/goutils`](https://github.com/minya/goutils) — HTTP utilities
- [`github.com/modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk) — MCP server SDK
- [`github.com/bogem/id3v2`](https://github.com/bogem/id3v2) — ID3 tags for the MCP tag tools
