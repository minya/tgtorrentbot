package main

import (
	"bytes"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
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
	before := loadTestConfig(t, "old-leaked-token", "fixed-jwt-secret")
	tok := accessToken(t, before)
	if got := bearerStatus(before, tok); got != http.StatusOK {
		t.Fatalf("token on its own server: %d, want 200", got)
	}

	after := loadTestConfig(t, "new-rotated-token", "fixed-jwt-secret")
	if bytes.Equal(before.JWTSecret, after.JWTSecret) {
		t.Fatal("signing key unchanged after rotating TGT_MCP_TOKEN")
	}
	if got := bearerStatus(after, tok); got != http.StatusUnauthorized {
		t.Fatalf("old token after rotating TGT_MCP_TOKEN: %d, want 401", got)
	}
}

func TestRotatingJWTSecretRevokes(t *testing.T) {
	before := loadTestConfig(t, "token", "jwt-secret-1")
	tok := accessToken(t, before)
	after := loadTestConfig(t, "token", "jwt-secret-2")
	if got := bearerStatus(after, tok); got != http.StatusUnauthorized {
		t.Fatalf("old token after rotating TGT_MCP_JWT_SECRET: %d, want 401", got)
	}
	// Setting a JWT secret where there was none also revokes.
	unset := loadTestConfig(t, "token", "")
	if bytes.Equal(unset.JWTSecret, before.JWTSecret) {
		t.Fatal("explicit JWT secret gives the same key as the default")
	}
}

func TestJWTSecretStableWithoutRotation(t *testing.T) {
	a := loadTestConfig(t, "token", "jwt")
	b := loadTestConfig(t, "token", "jwt")
	if !bytes.Equal(a.JWTSecret, b.JWTSecret) {
		t.Fatal("same env gives different signing keys")
	}
}

// Without TGT_MCP_JWT_SECRET the key is unchanged from earlier releases, so
// upgrading does not log existing clients out.
func TestDefaultJWTSecretDerivationUnchanged(t *testing.T) {
	cfg := loadTestConfig(t, "token", "")
	want := sha256.Sum256([]byte("tgtorrentbot-mcp/jwt\x00token"))
	if !bytes.Equal(cfg.JWTSecret, want[:]) {
		t.Fatal("default JWT secret derivation changed")
	}
}
