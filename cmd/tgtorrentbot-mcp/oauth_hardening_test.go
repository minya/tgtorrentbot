package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func registerJSON(t *testing.T, ts *httptest.Server, body string) (int, registrationResponse) {
	t.Helper()
	resp, err := http.Post(ts.URL+"/register", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var reg registrationResponse
	_ = json.NewDecoder(resp.Body).Decode(&reg)
	return resp.StatusCode, reg
}

// F24: /register bounds the body and every field signed into the client_id.
func TestRegisterRejectsOversizedInput(t *testing.T) {
	_, ts := newTestHTTPServer(t)
	defer ts.Close()

	many := make([]string, maxRedirectURIs+1)
	for i := range many {
		many[i] = `"https://c.example.com/cb"`
	}
	for name, body := range map[string]string{
		"huge body":        `{"redirect_uris":["https://c.example.com/cb"],"client_name":"` + strings.Repeat("x", 1<<20) + `"}`,
		"long client_name": `{"redirect_uris":["https://c.example.com/cb"],"client_name":"` + strings.Repeat("x", maxClientNameLen+1) + `"}`,
		"too many uris":    `{"redirect_uris":[` + strings.Join(many, ",") + `]}`,
		"long uri":         `{"redirect_uris":["https://c.example.com/` + strings.Repeat("a", maxRedirectURILen) + `"]}`,
	} {
		if code, _ := registerJSON(t, ts, body); code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, code)
		}
	}
	if code, reg := registerJSON(t, ts, `{"redirect_uris":["https://c.example.com/cb"],"client_name":"Claude"}`); code != http.StatusCreated || reg.ClientID == "" {
		t.Fatalf("normal registration: status %d", code)
	}
}

func authorizeGetPage(t *testing.T, ts *httptest.Server, clientID, redirect string) (*http.Response, string) {
	t.Helper()
	sum := sha256.Sum256([]byte("verifier"))
	q := url.Values{
		"client_id": {clientID}, "redirect_uri": {redirect}, "response_type": {"code"},
		"state": {"s"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
	}
	resp, err := http.Get(ts.URL + "/authorize?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(body)
}

// F23: the login form names where the code goes and refuses to be framed.
func TestAuthorizeFormShowsRedirectHostAndDeniesFraming(t *testing.T) {
	_, ts := newTestHTTPServer(t)
	defer ts.Close()
	_, reg := registerJSON(t, ts, `{"redirect_uris":["https://attacker.example/cb"],"client_name":"Claude"}`)

	resp, body := authorizeGetPage(t, ts, reg.ClientID, "https://attacker.example/cb")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if !strings.Contains(body, "<strong>attacker.example</strong>") {
		t.Error("login form does not show the redirect host")
	}
	if resp.Header.Get("X-Frame-Options") != "DENY" {
		t.Errorf("X-Frame-Options = %q", resp.Header.Get("X-Frame-Options"))
	}
	if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Errorf("CSP = %q", resp.Header.Get("Content-Security-Policy"))
	}
}

func refreshWith(t *testing.T, ts *httptest.Server, clientID, refresh string) (int, tokenResponse) {
	t.Helper()
	resp, err := http.PostForm(ts.URL+"/token", url.Values{
		"grant_type": {"refresh_token"}, "client_id": {clientID}, "refresh_token": {refresh},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var tr tokenResponse
	_ = json.NewDecoder(resp.Body).Decode(&tr)
	return resp.StatusCode, tr
}

func refreshToken(t *testing.T, srv *server, claims jwtClaims) string {
	t.Helper()
	tok, err := signJWT(srv.config.JWTSecret, claims)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// F25: a refresh token minted for another issuer/audience is refused.
func TestRefreshChecksIssuerAndAudience(t *testing.T) {
	srv, ts := newTestHTTPServer(t)
	defer ts.Close()
	now := time.Now()
	forged := refreshToken(t, srv, jwtClaims{Iss: "https://other", Aud: "https://other", Typ: "refresh",
		ClientID: "c", Iat: now.Unix(), AuthTime: now.Unix(), Exp: now.Add(time.Hour).Unix()})
	if code, _ := refreshWith(t, ts, "c", forged); code != http.StatusBadRequest {
		t.Fatalf("foreign iss/aud: status %d, want 400", code)
	}
}

// F25: refreshing carries the login time forward, so a session cannot be
// renewed past refreshTokenTTL after login, however often it is refreshed.
func TestRefreshCannotOutliveLogin(t *testing.T) {
	srv, ts := newTestHTTPServer(t)
	defer ts.Close()
	base := srv.config.PublicBaseURL
	now := time.Now()

	login := now.Add(-refreshTokenTTL + time.Hour) // logged in 29 days 23 h ago
	tok := refreshToken(t, srv, jwtClaims{Iss: base, Aud: base, Typ: "refresh", ClientID: "c",
		Iat: now.Unix(), AuthTime: login.Unix(), Exp: now.Add(time.Hour).Unix()})
	code, tr := refreshWith(t, ts, "c", tok)
	if code != http.StatusOK {
		t.Fatalf("refresh within the session: status %d", code)
	}
	claims, err := verifyJWT(srv.config.JWTSecret, tr.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if claims.AuthTime != login.Unix() {
		t.Errorf("new refresh token auth_time = %d, want the original %d", claims.AuthTime, login.Unix())
	}
	if want := login.Add(refreshTokenTTL).Unix(); claims.Exp != want {
		t.Errorf("new refresh token exp = %d, want login+TTL %d", claims.Exp, want)
	}

	expired := refreshToken(t, srv, jwtClaims{Iss: base, Aud: base, Typ: "refresh", ClientID: "c",
		Iat: now.Unix(), AuthTime: now.Add(-refreshTokenTTL - time.Minute).Unix(), Exp: now.Add(time.Hour).Unix()})
	if code, _ := refreshWith(t, ts, "c", expired); code != http.StatusBadRequest {
		t.Fatalf("refresh past login+TTL: status %d, want 400", code)
	}
}

// Refresh tokens minted before auth_time existed count from their iat.
func TestRefreshLegacyTokenCountsFromIat(t *testing.T) {
	srv, ts := newTestHTTPServer(t)
	defer ts.Close()
	base := srv.config.PublicBaseURL
	now := time.Now()
	old := refreshToken(t, srv, jwtClaims{Iss: base, Aud: base, Typ: "refresh", ClientID: "c",
		Iat: now.Add(-refreshTokenTTL - time.Minute).Unix(), Exp: now.Add(time.Hour).Unix()})
	if code, _ := refreshWith(t, ts, "c", old); code != http.StatusBadRequest {
		t.Fatalf("legacy token older than TTL: status %d, want 400", code)
	}
	recent := refreshToken(t, srv, jwtClaims{Iss: base, Aud: base, Typ: "refresh", ClientID: "c",
		Iat: now.Add(-time.Hour).Unix(), Exp: now.Add(time.Hour).Unix()})
	if code, _ := refreshWith(t, ts, "c", recent); code != http.StatusOK {
		t.Fatalf("recent legacy token: status %d, want 200", code)
	}
}
