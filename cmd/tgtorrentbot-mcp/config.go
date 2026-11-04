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
	// It depends on both TGT_MCP_JWT_SECRET and AuthToken, so rotating
	// either env var invalidates every outstanding token (see
	// deriveJWTSecret).
	JWTSecret []byte
}

// resolvedDir returns p with symlinks resolved, or p itself when it does
// not exist (yet).
func resolvedDir(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
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
	// Resolve symlinks in the root once. The path guards compare resolved
	// paths against it and the tools report paths relative to it, so an
	// unresolved root (e.g. /downloads -> /mnt/disk/downloads) would make
	// every returned path absolute and then rejected when passed back.
	dl = resolvedDir(filepath.Clean(abs))

	inc := os.Getenv("TGT_INCOMPLETE_PATH")
	if inc == "" {
		inc = filepath.Join(dl, "incomplete")
	} else if incAbs, err := filepath.Abs(inc); err == nil {
		inc = resolvedDir(filepath.Clean(incAbs))
	}

	authToken := os.Getenv("TGT_MCP_TOKEN")
	publicURL := strings.TrimRight(os.Getenv("TGT_MCP_PUBLIC_URL"), "/")
	if authToken != "" && publicURL == "" {
		return Config{}, fmt.Errorf("TGT_MCP_PUBLIC_URL is required when TGT_MCP_TOKEN is set")
	}

	// The signing key must not come from the login secret alone: /register
	// hands any caller a client_id signed with it, so a key derived only
	// from a human-typed secret lets that secret be brute-forced offline,
	// past every login rate limit. HTTP mode therefore needs a high-entropy
	// TGT_MCP_JWT_SECRET (e.g. `openssl rand -base64 48`).
	var jwtSecret []byte
	if authToken != "" {
		key := os.Getenv("TGT_MCP_JWT_SECRET")
		if len(key) < minJWTSecretLen {
			return Config{}, fmt.Errorf("TGT_MCP_JWT_SECRET must be set to at least %d random characters when TGT_MCP_TOKEN is set (e.g. openssl rand -base64 48)", minJWTSecretLen)
		}
		jwtSecret = deriveJWTSecret(authToken, key)
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
		JWTSecret:               jwtSecret,
	}, nil
}

// jwtSecretLabel domain-separates the JWT signing key from other uses of the
// login secret.
const jwtSecretLabel = "tgtorrentbot-mcp/jwt\x00"

// minJWTSecretLen is the shortest TGT_MCP_JWT_SECRET accepted: 32
// characters, at least 192 bits from a base64 random string.
const minJWTSecretLen = 32

// deriveJWTSecret returns the JWT signing key,
// HMAC-SHA256(key=jwtSecret, label || authToken). It depends on both: the
// high-entropy jwtSecret keeps the login secret from being guessed offline
// from a signed token, and mixing in authToken means rotating
// TGT_MCP_TOKEN after a leak revokes every token, as rotating
// TGT_MCP_JWT_SECRET does.
func deriveJWTSecret(authToken, jwtSecret string) []byte {
	mac := hmac.New(sha256.New, []byte(jwtSecret))
	mac.Write([]byte(jwtSecretLabel + authToken))
	return mac.Sum(nil)
}
