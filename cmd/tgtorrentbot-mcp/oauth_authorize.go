package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

// authCode is one in-memory pending authorization. TTL is short (60s) so we
// don't bother persisting; a lost map on restart just forces the user to
// re-redirect, which is cheap.
type authCode struct {
	ClientID      string
	RedirectURI   string
	CodeChallenge string
	Scope         string
	Exp           time.Time
}

// codeStore is a mutex-guarded map. Single-user server, no need for anything
// fancier; we sweep expired entries opportunistically on access.
type codeStore struct {
	mu    sync.Mutex
	codes map[string]authCode
}

func newCodeStore() *codeStore { return &codeStore{codes: make(map[string]authCode)} }

func (s *codeStore) put(code string, ac authCode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	s.codes[code] = ac
}

// take atomically returns and removes a code — single-use.
func (s *codeStore) take(code string) (authCode, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ac, ok := s.codes[code]
	if !ok {
		return authCode{}, false
	}
	delete(s.codes, code)
	if time.Now().After(ac.Exp) {
		return authCode{}, false
	}
	return ac, true
}

func (s *codeStore) sweepLocked() {
	now := time.Now()
	for k, v := range s.codes {
		if now.After(v.Exp) {
			delete(s.codes, k)
		}
	}
}

// rateLimiter is a laughably simple per-IP sliding window for /authorize POSTs.
// Good enough to slow down a human brute-forcing the login form; real DoS
// protection is Cloudflare's job.
type rateLimiter struct {
	mu      sync.Mutex
	hits    map[string][]time.Time
	limit   int
	window  time.Duration
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{hits: make(map[string][]time.Time), limit: limit, window: window}
}

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-rl.window)
	hits := rl.hits[key]
	kept := hits[:0]
	for _, t := range hits {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= rl.limit {
		rl.hits[key] = kept
		return false
	}
	kept = append(kept, now)
	rl.hits[key] = kept
	return true
}

// clientIP extracts a best-effort identifier; we're behind Cloudflare so trust
// the tunnel-provided X-Forwarded-For first, fall back to RemoteAddr.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

const authorizeFormHTML = `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>Authorize tgtorrentbot-mcp</title>
<style>
body{font-family:system-ui,sans-serif;background:#111;color:#eee;display:flex;align-items:center;justify-content:center;height:100vh;margin:0}
.card{background:#1b1b1b;padding:2rem;border-radius:8px;max-width:380px;width:90%;box-shadow:0 2px 12px rgba(0,0,0,.5)}
h1{margin:0 0 .5rem;font-size:1.2rem}
p{color:#aaa;font-size:.9rem;margin:0 0 1rem}
input[type=password]{width:100%;padding:.6rem;border:1px solid #333;background:#0d0d0d;color:#eee;border-radius:4px;box-sizing:border-box}
button{margin-top:1rem;width:100%;padding:.6rem;background:#3b82f6;color:#fff;border:0;border-radius:4px;cursor:pointer;font-size:1rem}
.err{color:#f87171;font-size:.85rem;margin-top:.5rem}
code{background:#222;padding:.1rem .3rem;border-radius:3px}
</style></head><body>
<form class="card" method="POST" action="/authorize">
<h1>Authorize <code>{{.ClientName}}</code></h1>
<p>Enter the MCP shared secret to grant access.</p>
<input type="password" name="secret" autocomplete="off" autofocus required>
{{if .Error}}<div class="err">{{.Error}}</div>{{end}}
<input type="hidden" name="response_type" value="{{.ResponseType}}">
<input type="hidden" name="client_id" value="{{.ClientID}}">
<input type="hidden" name="redirect_uri" value="{{.RedirectURI}}">
<input type="hidden" name="state" value="{{.State}}">
<input type="hidden" name="code_challenge" value="{{.CodeChallenge}}">
<input type="hidden" name="code_challenge_method" value="{{.CodeChallengeMethod}}">
<input type="hidden" name="scope" value="{{.Scope}}">
<button type="submit">Authorize</button>
</form></body></html>`

var authorizeFormTmpl = template.Must(template.New("authorize").Parse(authorizeFormHTML))

type authorizeFormData struct {
	ResponseType        string
	ClientID            string
	ClientName          string
	RedirectURI         string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
	Scope               string
	Error               string
}

func (s *server) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleAuthorizeGet(w, r)
	case http.MethodPost:
		s.handleAuthorizePost(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// validateAuthorizeParams pulls params from a Values map (query or form) and
// runs every OAuth 2.1 check that is independent of the user secret. Returns
// the resolved client metadata plus the incoming params as a struct. On
// validation failure it has already written the error response.
func (s *server) validateAuthorizeParams(w http.ResponseWriter, r *http.Request, v url.Values) (jwtClaims, authorizeFormData, bool) {
	data := authorizeFormData{
		ResponseType:        v.Get("response_type"),
		ClientID:            v.Get("client_id"),
		RedirectURI:         v.Get("redirect_uri"),
		State:               v.Get("state"),
		CodeChallenge:       v.Get("code_challenge"),
		CodeChallengeMethod: v.Get("code_challenge_method"),
		Scope:               v.Get("scope"),
	}

	if data.ClientID == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "client_id is required")
		return jwtClaims{}, data, false
	}
	client, err := s.decodeClientID(data.ClientID)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client", "client_id is invalid")
		return jwtClaims{}, data, false
	}
	if data.RedirectURI == "" || !slices.Contains(client.RedirectURIs, data.RedirectURI) {
		// Per RFC 6749 §3.1.2.4, redirect_uri errors must NOT redirect back;
		// render directly.
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "redirect_uri does not match any registered URI")
		return jwtClaims{}, data, false
	}

	// From here on, errors are OAuth errors returned via redirect.
	if data.ResponseType != "code" {
		redirectOAuthError(w, r, data.RedirectURI, data.State, "unsupported_response_type", "only response_type=code is supported")
		return jwtClaims{}, data, false
	}
	if data.CodeChallenge == "" {
		redirectOAuthError(w, r, data.RedirectURI, data.State, "invalid_request", "code_challenge is required (PKCE)")
		return jwtClaims{}, data, false
	}
	if data.CodeChallengeMethod != "S256" {
		redirectOAuthError(w, r, data.RedirectURI, data.State, "invalid_request", "code_challenge_method must be S256")
		return jwtClaims{}, data, false
	}

	data.ClientName = client.ClientName
	if data.ClientName == "" {
		data.ClientName = "MCP client"
	}
	return client, data, true
}

func (s *server) handleAuthorizeGet(w http.ResponseWriter, r *http.Request) {
	_, data, ok := s.validateAuthorizeParams(w, r, r.URL.Query())
	if !ok {
		return
	}
	renderAuthorizeForm(w, http.StatusOK, data)
}

func (s *server) handleAuthorizePost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "malformed form")
		return
	}
	_, data, ok := s.validateAuthorizeParams(w, r, r.PostForm)
	if !ok {
		return
	}

	if !s.loginLimiter.allow(clientIP(r)) {
		data.Error = "Too many attempts. Try again in a minute."
		renderAuthorizeForm(w, http.StatusTooManyRequests, data)
		return
	}

	secret := r.PostForm.Get("secret")
	if subtle.ConstantTimeCompare([]byte(secret), []byte(s.config.AuthToken)) != 1 {
		data.Error = "Incorrect secret."
		renderAuthorizeForm(w, http.StatusUnauthorized, data)
		return
	}

	code, err := randomToken(32)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to mint code")
		return
	}
	s.codeStore.put(code, authCode{
		ClientID:      data.ClientID,
		RedirectURI:   data.RedirectURI,
		CodeChallenge: data.CodeChallenge,
		Scope:         data.Scope,
		Exp:           time.Now().Add(60 * time.Second),
	})

	// Build redirect with code + state.
	u, _ := url.Parse(data.RedirectURI)
	q := u.Query()
	q.Set("code", code)
	if data.State != "" {
		q.Set("state", data.State)
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func renderAuthorizeForm(w http.ResponseWriter, status int, data authorizeFormData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = authorizeFormTmpl.Execute(w, data)
}

func redirectOAuthError(w http.ResponseWriter, r *http.Request, redirectURI, state, code, desc string) {
	u, err := url.Parse(redirectURI)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, code, desc)
		return
	}
	q := u.Query()
	q.Set("error", code)
	if desc != "" {
		q.Set("error_description", desc)
	}
	if state != "" {
		q.Set("state", state)
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func randomToken(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
