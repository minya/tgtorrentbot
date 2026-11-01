package main

import (
	"crypto/hmac"
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
	// AudiobookshelfLibraryID, when set, is the default library for
	// abs_remove_issues; otherwise the single book-type library is looked up.
	AudiobookshelfLibraryID string
	// AuthToken, when non-empty, switches the server to HTTP transport on :8080
	// and requires OAuth 2.1. The token doubles as the shared secret the user
	// types into the /authorize login form. Empty means stdio.
	AuthToken string
	// PublicBaseURL is the externally-reachable HTTPS base (e.g.
	// "https://mcp.example.com"). Required in HTTP mode: it is the OAuth
	// issuer and audience, and prefixes every endpoint advertised in metadata.
	PublicBaseURL string
	// JWTSecret signs every token we issue (access, refresh, client_id).
	// It always depends on AuthToken, and also on TGT_MCP_JWT_SECRET when
	// set, so rotating either env var invalidates every outstanding token
	// (see deriveJWTSecret).
	JWTSecret []byte
	// RedirectHosts, from the comma-separated TGT_MCP_REDIRECT_HOSTS, limits
	// OAuth redirect URIs to these hosts (lowercase; loopback is always
	// allowed). Empty allows any https host.
	RedirectHosts []string
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
		jwtSecret = deriveJWTSecret(authToken, os.Getenv("TGT_MCP_JWT_SECRET"))
	}

	return Config{
		DownloadPath:            dl,
		IncompletePath:          inc,
		TransmissionAddr:        os.Getenv("TGT_RPC_ADDR"),
		TransmissionUser:        os.Getenv("TGT_RPC_USER"),
		TransmissionPassword:    os.Getenv("TGT_RPC_PASSWORD"),
		JellyfinURL:             os.Getenv("TGT_JELLYFIN_URL"),
		JellyfinAPIKey:          os.Getenv("TGT_JELLYFIN_API_KEY"),
		AudiobookshelfURL:       os.Getenv("TGT_AUDIOBOOKSHELF_URL"),
		AudiobookshelfAPIKey:    os.Getenv("TGT_AUDIOBOOKSHELF_API_KEY"),
		AudiobookshelfLibraryID: os.Getenv("TGT_AUDIOBOOKSHELF_LIBRARY_ID"),
		AuthToken:               authToken,
		PublicBaseURL:           publicURL,
		RedirectHosts:           parseHostList(os.Getenv("TGT_MCP_REDIRECT_HOSTS")),
		JWTSecret:               jwtSecret,
	}, nil
}

// jwtSecretLabel domain-separates the JWT signing key from other uses of the
// login secret.
const jwtSecretLabel = "tgtorrentbot-mcp/jwt\x00"

// deriveJWTSecret returns the JWT signing key. Without an explicit JWT secret
// it is SHA-256(label || authToken); with one it is
// HMAC-SHA256(key=jwtSecret, label || authToken). Either way it depends on the
// login secret, so rotating TGT_MCP_TOKEN after a leak revokes every token
// even when TGT_MCP_JWT_SECRET is set, and rotating TGT_MCP_JWT_SECRET
// revokes them without changing the login secret.
func deriveJWTSecret(authToken, jwtSecret string) []byte {
	if jwtSecret == "" {
		h := sha256.Sum256([]byte(jwtSecretLabel + authToken))
		return h[:]
	}
	mac := hmac.New(sha256.New, []byte(jwtSecret))
	mac.Write([]byte(jwtSecretLabel + authToken))
	return mac.Sum(nil)
}

// parseHostList splits a comma-separated host list, lowercasing and dropping
// empty entries.
func parseHostList(s string) []string {
	var hosts []string
	for _, h := range strings.Split(s, ",") {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			hosts = append(hosts, h)
		}
	}
	return hosts
}
