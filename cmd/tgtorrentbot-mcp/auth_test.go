package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

// testServer builds a *server wired with everything the HTTP-mode code needs
// so tests can call the OAuth handlers directly. The PublicBaseURL points at
// the httptest server so issuer/aud checks line up.
func testServer(t *testing.T, base string) *server {
	t.Helper()
	cfg := Config{
		DownloadPath:  t.TempDir(),
		AuthToken:     "hunter2",
		PublicBaseURL: base,
		JWTSecret:     []byte("test-secret-at-least-32-bytes-long!!"),
	}
	return &server{
		config:       cfg,
		codeStore:    newCodeStore(),
		loginLimiter: newRateLimiter(100, time.Minute),
	}
}

func newTestHTTPServer(t *testing.T) (*server, *httptest.Server) {
	t.Helper()
	srv := testServer(t, "")
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-authorization-server", srv.handleAuthServerMetadata)
	mux.HandleFunc("/.well-known/oauth-protected-resource", srv.handleProtectedResourceMetadata)
	mux.HandleFunc("/register", srv.handleRegister)
	mux.HandleFunc("/authorize", srv.handleAuthorize)
	mux.HandleFunc("/token", srv.handleToken)
	ts := httptest.NewServer(mux)
	srv.config.PublicBaseURL = ts.URL
	return srv, ts
}

func TestJWTRoundTrip(t *testing.T) {
	secret := []byte("the-secret")
	claims := jwtClaims{
		Iss: "me", Aud: "me", Sub: "x",
		Iat: time.Now().Unix(),
		Exp: time.Now().Add(time.Hour).Unix(),
		Typ: "access",
	}
	tok, err := signJWT(secret, claims)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	got, err := verifyJWT(secret, tok)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got.Sub != "x" || got.Typ != "access" {
		t.Fatalf("claims not preserved: %+v", got)
	}
}

func TestJWTTampered(t *testing.T) {
	tok, _ := signJWT([]byte("a"), jwtClaims{Exp: time.Now().Add(time.Hour).Unix()})
	if _, err := verifyJWT([]byte("b"), tok); err == nil {
		t.Fatal("accepted wrong signing key")
	}
	// Swap a char in the signature segment.
	parts := strings.Split(tok, ".")
	parts[2] = strings.Repeat("A", len(parts[2]))
	if _, err := verifyJWT([]byte("a"), strings.Join(parts, ".")); err == nil {
		t.Fatal("accepted tampered signature")
	}
}

func TestJWTExpired(t *testing.T) {
	tok, _ := signJWT([]byte("a"), jwtClaims{Exp: time.Now().Add(-time.Second).Unix()})
	if _, err := verifyJWT([]byte("a"), tok); err == nil {
		t.Fatal("accepted expired token")
	}
}

func TestPKCEVerifier(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	if !verifyPKCE(challenge, verifier) {
		t.Fatal("should accept matching verifier")
	}
	if verifyPKCE(challenge, verifier+"x") {
		t.Fatal("should reject mismatched verifier")
	}
}

func TestMetadataEndpoints(t *testing.T) {
	_, ts := newTestHTTPServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/.well-known/oauth-authorization-server")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var m authServerMetadata
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatal(err)
	}
	if m.Issuer != ts.URL {
		t.Errorf("issuer = %q, want %q", m.Issuer, ts.URL)
	}
	if m.RegistrationEndpoint != ts.URL+"/register" {
		t.Errorf("registration = %q", m.RegistrationEndpoint)
	}
}

func TestDCRRegister(t *testing.T) {
	_, ts := newTestHTTPServer(t)
	defer ts.Close()

	body := strings.NewReader(`{"redirect_uris":["https://example.com/cb"],"client_name":"test"}`)
	resp, err := http.Post(ts.URL+"/register", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d, body %s", resp.StatusCode, b)
	}
	var rr registrationResponse
	if err := json.NewDecoder(resp.Body).Decode(&rr); err != nil {
		t.Fatal(err)
	}
	if rr.ClientID == "" {
		t.Fatal("no client_id returned")
	}
	if len(rr.RedirectURIs) != 1 || rr.RedirectURIs[0] != "https://example.com/cb" {
		t.Fatalf("redirect_uris = %v", rr.RedirectURIs)
	}
}

func TestDCRRejectsNonHTTPSRedirect(t *testing.T) {
	_, ts := newTestHTTPServer(t)
	defer ts.Close()

	body := strings.NewReader(`{"redirect_uris":["http://evil.example.com/cb"]}`)
	resp, err := http.Post(ts.URL+"/register", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
}

// TestFullOAuthFlow does DCR → /authorize (GET form + POST secret) → /token
// (code grant) → verify access token → refresh → verify new access token.
func TestFullOAuthFlow(t *testing.T) {
	srv, ts := newTestHTTPServer(t)
	defer ts.Close()

	// 1. DCR
	redirect := "https://client.example.com/cb"
	regBody := strings.NewReader(`{"redirect_uris":["` + redirect + `"],"client_name":"claude"}`)
	regResp, _ := http.Post(ts.URL+"/register", "application/json", regBody)
	var reg registrationResponse
	json.NewDecoder(regResp.Body).Decode(&reg)
	regResp.Body.Close()
	if reg.ClientID == "" {
		t.Fatal("dcr failed")
	}

	// 2. Build PKCE pair.
	verifier := "a-verifier-long-enough-to-be-valid-per-oauth"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	// 3. GET /authorize — should render the form.
	getURL := ts.URL + "/authorize?" + url.Values{
		"response_type":         {"code"},
		"client_id":             {reg.ClientID},
		"redirect_uri":          {redirect},
		"state":                 {"xyz"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}.Encode()
	getResp, _ := http.Get(getURL)
	body, _ := io.ReadAll(getResp.Body)
	getResp.Body.Close()
	if getResp.StatusCode != 200 || !strings.Contains(string(body), "MCP shared secret") {
		t.Fatalf("GET /authorize: %d\n%s", getResp.StatusCode, body)
	}

	// 4. POST /authorize with correct secret — should 302 with code.
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	postResp, err := client.PostForm(ts.URL+"/authorize", url.Values{
		"client_id":             {reg.ClientID},
		"redirect_uri":          {redirect},
		"state":                 {"xyz"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"response_type":         {"code"},
		"secret":                {srv.config.AuthToken},
	})
	if err != nil {
		t.Fatal(err)
	}
	if postResp.StatusCode != http.StatusFound {
		b, _ := io.ReadAll(postResp.Body)
		t.Fatalf("want 302, got %d\n%s", postResp.StatusCode, b)
	}
	loc, _ := url.Parse(postResp.Header.Get("Location"))
	code := loc.Query().Get("code")
	if code == "" {
		t.Fatalf("no code in redirect: %s", postResp.Header.Get("Location"))
	}
	if loc.Query().Get("state") != "xyz" {
		t.Errorf("state not echoed")
	}

	// 5. Exchange code for tokens.
	tokResp, err := http.PostForm(ts.URL+"/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {reg.ClientID},
		"redirect_uri":  {redirect},
		"code_verifier": {verifier},
	})
	if err != nil {
		t.Fatal(err)
	}
	if tokResp.StatusCode != 200 {
		b, _ := io.ReadAll(tokResp.Body)
		t.Fatalf("token: %d\n%s", tokResp.StatusCode, b)
	}
	var tok tokenResponse
	json.NewDecoder(tokResp.Body).Decode(&tok)
	tokResp.Body.Close()
	if tok.AccessToken == "" || tok.RefreshToken == "" {
		t.Fatalf("missing tokens: %+v", tok)
	}

	// 6. Verify access token is usable via jwtAuth.
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	h := srv.jwtAuth(inner)
	req := httptest.NewRequest("POST", "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("jwtAuth rejected freshly-minted access token: %d", rec.Code)
	}

	// 7. Refresh.
	refResp, err := http.PostForm(ts.URL+"/token", url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {tok.RefreshToken},
		"client_id":     {reg.ClientID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if refResp.StatusCode != 200 {
		b, _ := io.ReadAll(refResp.Body)
		t.Fatalf("refresh: %d\n%s", refResp.StatusCode, b)
	}
	var tok2 tokenResponse
	json.NewDecoder(refResp.Body).Decode(&tok2)
	refResp.Body.Close()
	if tok2.AccessToken == "" || tok2.AccessToken == tok.AccessToken {
		t.Fatal("refresh did not produce a new access token")
	}
}

func TestAuthorizeRejectsBadSecret(t *testing.T) {
	srv, ts := newTestHTTPServer(t)
	defer ts.Close()

	// Register first.
	regResp, _ := http.Post(ts.URL+"/register", "application/json",
		strings.NewReader(`{"redirect_uris":["https://c.example.com/cb"]}`))
	var reg registrationResponse
	json.NewDecoder(regResp.Body).Decode(&reg)
	regResp.Body.Close()

	verifier := "v"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	resp, err := http.PostForm(ts.URL+"/authorize", url.Values{
		"client_id":             {reg.ClientID},
		"redirect_uri":          {"https://c.example.com/cb"},
		"state":                 {"s"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"response_type":         {"code"},
		"secret":                {"wrong"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", resp.StatusCode)
	}
	_ = srv
}

func TestTokenRejectsMismatchedVerifier(t *testing.T) {
	srv, ts := newTestHTTPServer(t)
	defer ts.Close()

	regResp, _ := http.Post(ts.URL+"/register", "application/json",
		strings.NewReader(`{"redirect_uris":["https://c.example.com/cb"]}`))
	var reg registrationResponse
	json.NewDecoder(regResp.Body).Decode(&reg)
	regResp.Body.Close()

	verifier := "v"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	postResp, _ := client.PostForm(ts.URL+"/authorize", url.Values{
		"client_id":             {reg.ClientID},
		"redirect_uri":          {"https://c.example.com/cb"},
		"state":                 {"s"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"response_type":         {"code"},
		"secret":                {srv.config.AuthToken},
	})
	loc, _ := url.Parse(postResp.Header.Get("Location"))
	code := loc.Query().Get("code")
	postResp.Body.Close()

	resp, err := http.PostForm(ts.URL+"/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {reg.ClientID},
		"redirect_uri":  {"https://c.example.com/cb"},
		"code_verifier": {"wrong-verifier"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
}

// TestAuthorizeFormCarriesAllOAuthParams renders the form like a real browser
// would (GET with query), scrapes the hidden inputs, then POSTs only those
// inputs. Guards against the regression where a missing hidden field makes
// claude.ai's callback see "code: Field required" because we redirected with
// an error instead of a code.
func TestAuthorizeFormCarriesAllOAuthParams(t *testing.T) {
	srv, ts := newTestHTTPServer(t)
	defer ts.Close()

	// Register.
	regResp, _ := http.Post(ts.URL+"/register", "application/json",
		strings.NewReader(`{"redirect_uris":["https://c.example.com/cb"]}`))
	var reg registrationResponse
	json.NewDecoder(regResp.Body).Decode(&reg)
	regResp.Body.Close()

	verifier := "v-that-is-long-enough-for-pkce"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	// 1. GET the form, like a browser following the auth redirect.
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {reg.ClientID},
		"redirect_uri":          {"https://c.example.com/cb"},
		"state":                 {"xyz"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	resp, err := http.Get(ts.URL + "/authorize?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	// 2. Scrape hidden inputs — every param the browser would resubmit.
	re := regexp.MustCompile(`<input type="hidden" name="([^"]+)" value="([^"]*)"`)
	form := url.Values{}
	for _, m := range re.FindAllStringSubmatch(string(body), -1) {
		form.Set(m[1], m[2])
	}
	form.Set("secret", srv.config.AuthToken)

	// Sanity: every OAuth param from the query should have survived the render.
	for _, k := range []string{"response_type", "client_id", "redirect_uri", "state", "code_challenge", "code_challenge_method"} {
		if form.Get(k) != q.Get(k) {
			t.Errorf("form hidden %q = %q, want %q (hidden input missing in template?)",
				k, form.Get(k), q.Get(k))
		}
	}

	// 3. POST exactly those fields — should 302 with code, not error.
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	postResp, err := client.PostForm(ts.URL+"/authorize", form)
	if err != nil {
		t.Fatal(err)
	}
	defer postResp.Body.Close()
	if postResp.StatusCode != http.StatusFound {
		b, _ := io.ReadAll(postResp.Body)
		t.Fatalf("want 302, got %d\n%s", postResp.StatusCode, b)
	}
	loc, _ := url.Parse(postResp.Header.Get("Location"))
	if loc.Query().Get("error") != "" {
		t.Fatalf("form submission produced OAuth error: %s", loc.String())
	}
	if loc.Query().Get("code") == "" {
		t.Fatalf("no code in redirect: %s", loc.String())
	}
}

func TestJWTAuthRejectsNonAccessTypeToken(t *testing.T) {
	srv := testServer(t, "https://srv.example.com")
	// Mint a "refresh" typ token and try to use it as Bearer — must fail.
	refresh, _ := signJWT(srv.config.JWTSecret, jwtClaims{
		Iss: srv.config.PublicBaseURL, Aud: srv.config.PublicBaseURL,
		Iat: time.Now().Unix(),
		Exp: time.Now().Add(time.Hour).Unix(),
		Typ: "refresh",
	})
	h := srv.jwtAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	req := httptest.NewRequest("GET", "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+refresh)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}
