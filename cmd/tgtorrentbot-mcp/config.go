package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config is the subset of env config the MCP server needs. It intentionally
// omits Telegram/Rutracker settings that only the bot+webapp need.
type Config struct {
	DownloadPath         string
	IncompletePath       string
	TransmissionAddr     string
	TransmissionUser     string
	TransmissionPassword string
	JellyfinURL          string
	JellyfinAPIKey       string
	AudiobookshelfURL    string
	AudiobookshelfAPIKey string
	// AuthToken, when non-empty, switches the server to HTTP transport on :8080
	// and requires OAuth 2.1. The token doubles as the shared secret the user
	// types into the /authorize login form. Empty means stdio.
	AuthToken string
	// PublicBaseURL is the externally-reachable HTTPS base (e.g.
	// "https://mcp.example.com"). Required in HTTP mode: it is the OAuth
	// issuer and audience, and prefixes every endpoint advertised in metadata.
	PublicBaseURL string
	// JWTSecret signs every token we issue (access, refresh, client_id).
	// Derived from AuthToken via SHA-256 when unset so single-secret
	// deployments Just Work; override to rotate without touching AuthToken.
	JWTSecret []byte
}

func loadConfig() (Config, error) {
	dl := os.Getenv("TGT_DOWNLOADPATH")
	if dl == "" {
		return Config{}, fmt.Errorf("TGT_DOWNLOADPATH is not set")
	}
	abs, err := filepath.Abs(dl)
	if err != nil {
		return Config{}, fmt.Errorf("TGT_DOWNLOADPATH: %w", err)
	}
	dl = filepath.Clean(abs)

	inc := os.Getenv("TGT_INCOMPLETE_PATH")
	if inc == "" {
		inc = filepath.Join(dl, "incomplete")
	}

	authToken := os.Getenv("TGT_MCP_TOKEN")
	publicURL := strings.TrimRight(os.Getenv("TGT_MCP_PUBLIC_URL"), "/")
	if authToken != "" && publicURL == "" {
		return Config{}, fmt.Errorf("TGT_MCP_PUBLIC_URL is required when TGT_MCP_TOKEN is set")
	}

	var jwtSecret []byte
	if authToken != "" {
		if v := os.Getenv("TGT_MCP_JWT_SECRET"); v != "" {
			jwtSecret = []byte(v)
		} else {
			h := sha256.Sum256([]byte("tgtorrentbot-mcp/jwt\x00" + authToken))
			jwtSecret = h[:]
		}
	}

	return Config{
		DownloadPath:         dl,
		IncompletePath:       inc,
		TransmissionAddr:     os.Getenv("TGT_RPC_ADDR"),
		TransmissionUser:     os.Getenv("TGT_RPC_USER"),
		TransmissionPassword: os.Getenv("TGT_RPC_PASSWORD"),
		JellyfinURL:          os.Getenv("TGT_JELLYFIN_URL"),
		JellyfinAPIKey:       os.Getenv("TGT_JELLYFIN_API_KEY"),
		AudiobookshelfURL:    os.Getenv("TGT_AUDIOBOOKSHELF_URL"),
		AudiobookshelfAPIKey: os.Getenv("TGT_AUDIOBOOKSHELF_API_KEY"),
		AuthToken:            authToken,
		PublicBaseURL:        publicURL,
		JWTSecret:            jwtSecret,
	}, nil
}
