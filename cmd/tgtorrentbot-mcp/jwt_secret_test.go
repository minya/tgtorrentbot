package main

import (
	"bytes"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func loadTestConfig(t *testing.T, token, jwtSecret string) Config {
	t.Helper()
	t.Setenv("TGT_DOWNLOADPATH", t.TempDir())
	t.Setenv("TGT_MCP_PUBLIC_URL", "https://mcp.example.com")
	t.Setenv("TGT_MCP_TOKEN", token)
	t.Setenv("TGT_MCP_JWT_SECRET", jwtSecret)
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func bearerStatus(cfg Config, tok string) int {
	srv := &server{config: cfg}
	h := srv.jwtAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func accessToken(t *testing.T, cfg Config) string {
	t.Helper()
	tok, err := signJWT(cfg.JWTSecret, jwtClaims{
		Iss: cfg.PublicBaseURL, Aud: cfg.PublicBaseURL, Sub: "owner", Typ: "access",
		Iat: time.Now().Unix(), Exp: time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// Rotating the login secret revokes tokens even when TGT_MCP_JWT_SECRET is set.
func TestRotatingMCPTokenRevokesWithJWTSecretSet(t *testing.T) {
	before := loadTestConfig(t, "old-leaked-token", jwtKey1)
	tok := accessToken(t, before)
	if got := bearerStatus(before, tok); got != http.StatusOK {
		t.Fatalf("token on its own server: %d, want 200", got)
	}

	after := loadTestConfig(t, "new-rotated-token", jwtKey1)
	if bytes.Equal(before.JWTSecret, after.JWTSecret) {
		t.Fatal("signing key unchanged after rotating TGT_MCP_TOKEN")
	}
	if got := bearerStatus(after, tok); got != http.StatusUnauthorized {
		t.Fatalf("old token after rotating TGT_MCP_TOKEN: %d, want 401", got)
	}
}

func TestRotatingJWTSecretRevokes(t *testing.T) {
	before := loadTestConfig(t, "token", jwtKey1)
	tok := accessToken(t, before)
	after := loadTestConfig(t, "token", jwtKey2)
	if got := bearerStatus(after, tok); got != http.StatusUnauthorized {
		t.Fatalf("old token after rotating TGT_MCP_JWT_SECRET: %d, want 401", got)
	}
}

func TestJWTSecretStableWithoutRotation(t *testing.T) {
	a := loadTestConfig(t, "token", jwtKey1)
	b := loadTestConfig(t, "token", jwtKey1)
	if !bytes.Equal(a.JWTSecret, b.JWTSecret) {
		t.Fatal("same env gives different signing keys")
	}
}

const (
	jwtKey1 = "0123456789abcdefghijklmnopqrstuvwxyz-key-1"
	jwtKey2 = "0123456789abcdefghijklmnopqrstuvwxyz-key-2"
)

// F87: HTTP mode refuses to start without a high-entropy JWT secret, because
// a key derived from the login secret alone lets anyone who calls /register
// guess that secret offline from the client_id it returns.
func TestHTTPModeRequiresStrongJWTSecret(t *testing.T) {
	t.Setenv("TGT_DOWNLOADPATH", t.TempDir())
	t.Setenv("TGT_MCP_PUBLIC_URL", "https://mcp.example.com")
	t.Setenv("TGT_MCP_TOKEN", "k7q2")
	for _, key := range []string{"", "short-secret"} {
		t.Setenv("TGT_MCP_JWT_SECRET", key)
		if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), "TGT_MCP_JWT_SECRET") {
			t.Errorf("TGT_MCP_JWT_SECRET=%q: err = %v, want a startup error", key, err)
		}
	}
	// stdio mode needs no JWT secret.
	t.Setenv("TGT_MCP_TOKEN", "")
	t.Setenv("TGT_MCP_JWT_SECRET", "")
	if _, err := loadConfig(); err != nil {
		t.Errorf("stdio mode: %v", err)
	}
}

// F87: a client_id from /register is no longer an offline oracle for the
// login secret: the key a guessed token gives does not verify it.
func TestClientIDDoesNotRevealLoginSecret(t *testing.T) {
	cfg := loadTestConfig(t, "k7q2", jwtKey1)
	for _, guess := range [][]byte{
		func() []byte { h := sha256.Sum256([]byte("tgtorrentbot-mcp/jwt\x00k7q2")); return h[:] }(), // the old derivation
		deriveJWTSecret("k7q2", ""), // HMAC with an empty key
	} {
		if bytes.Equal(guess, cfg.JWTSecret) {
			t.Fatal("the signing key can be computed from the login secret alone")
		}
	}
}
