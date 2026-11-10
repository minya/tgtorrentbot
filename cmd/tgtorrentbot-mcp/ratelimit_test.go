package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// authorizeAttempt posts the login form with secret, setting header=value
// when header is non-empty, and returns the status code.
func authorizeAttempt(t *testing.T, ts *httptest.Server, clientID, secret, header, value string) int {
	t.Helper()
	sum := sha256.Sum256([]byte("verifier"))
	form := url.Values{
		"client_id":             {clientID},
		"redirect_uri":          {"https://c.example.com/cb"},
		"state":                 {"s"},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
		"response_type":         {"code"},
		"secret":                {secret},
	}
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/authorize", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if header != "" {
		req.Header.Set(header, value)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func registerTestClient(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	resp, err := http.Post(ts.URL+"/register", "application/json",
		strings.NewReader(`{"redirect_uris":["https://c.example.com/cb"]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var reg registrationResponse
	if err := json.NewDecoder(resp.Body).Decode(&reg); err != nil {
		t.Fatal(err)
	}
	return reg.ClientID
}

// Rotating X-Forwarded-For must not buy extra guesses.
func TestLoginRateLimitIgnoresXForwardedFor(t *testing.T) {
	srv, ts := newTestHTTPServer(t)
	defer ts.Close()
	srv.loginLimiter = newRateLimiter(10, time.Minute)
	srv.failedLoginLimiter = newRateLimiter(1000, time.Minute)
	cid := registerTestClient(t, ts)

	limited := 0
	for i := range 50 {
		xff := fmt.Sprintf("10.0.%d.%d, 203.0.113.9", i/256, i%256)
		if authorizeAttempt(t, ts, cid, "wrong", "X-Forwarded-For", xff) == http.StatusTooManyRequests {
			limited++
		}
	}
	if limited != 40 {
		t.Fatalf("%d of 50 attempts with rotating X-Forwarded-For were rate-limited, want 40", limited)
	}
}

// The per-IP limit keys on CF-Connecting-IP, so distinct real clients are
// limited separately.
func TestLoginRateLimitPerCFConnectingIP(t *testing.T) {
	srv, ts := newTestHTTPServer(t)
	defer ts.Close()
	srv.loginLimiter = newRateLimiter(3, time.Minute)
	srv.failedLoginLimiter = newRateLimiter(1000, time.Minute)
	cid := registerTestClient(t, ts)

	for range 3 {
		authorizeAttempt(t, ts, cid, "wrong", "CF-Connecting-IP", "198.51.100.1")
	}
	if got := authorizeAttempt(t, ts, cid, "wrong", "CF-Connecting-IP", "198.51.100.1"); got != http.StatusTooManyRequests {
		t.Fatalf("4th attempt from the same IP: %d, want 429", got)
	}
	if got := authorizeAttempt(t, ts, cid, "wrong", "CF-Connecting-IP", "198.51.100.2"); got != http.StatusUnauthorized {
		t.Fatalf("first attempt from another IP: %d, want 401", got)
	}
}

// Failed logins are capped across all clients, so rotating any
// client-controlled identifier does not help; once capped, even the right
// secret is refused until the window passes.
func TestFailedLoginGlobalCap(t *testing.T) {
	srv, ts := newTestHTTPServer(t)
	defer ts.Close()
	srv.loginLimiter = newRateLimiter(1000, time.Minute)
	srv.failedLoginLimiter = newRateLimiter(5, time.Minute)
	cid := registerTestClient(t, ts)

	unauthorized := 0
	for i := range 20 {
		ip := fmt.Sprintf("198.51.100.%d", i)
		if authorizeAttempt(t, ts, cid, "wrong", "CF-Connecting-IP", ip) == http.StatusUnauthorized {
			unauthorized++
		}
	}
	if unauthorized != 5 {
		t.Fatalf("%d wrong secrets were checked, want 5", unauthorized)
	}
	if got := authorizeAttempt(t, ts, cid, "hunter2", "CF-Connecting-IP", "198.51.100.99"); got != http.StatusTooManyRequests {
		t.Fatalf("right secret while capped: %d, want 429", got)
	}
}

func TestSuccessfulLoginNotCountedAsFailure(t *testing.T) {
	srv, ts := newTestHTTPServer(t)
	defer ts.Close()
	srv.failedLoginLimiter = newRateLimiter(1, time.Minute)
	cid := registerTestClient(t, ts)

	for range 3 {
		if got := authorizeAttempt(t, ts, cid, "hunter2", "", ""); got != http.StatusFound {
			t.Fatalf("right secret: %d, want 302", got)
		}
	}
}

// Keys whose hits fell out of the window are evicted, so spoofed or
// one-off keys cannot grow the map without bound.
func TestRateLimiterEvictsStaleKeys(t *testing.T) {
	rl := newRateLimiter(10, 20*time.Millisecond)
	const batch = rateLimiterSweepAt + 100
	for i := range batch {
		rl.allow(fmt.Sprintf("old%d", i))
	}
	time.Sleep(30 * time.Millisecond)
	for i := range batch {
		rl.allow(fmt.Sprintf("new%d", i))
	}
	if n := rl.size(); n > batch {
		t.Fatalf("limiter holds %d keys, want at most %d (the stale batch must be evicted)", n, batch)
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()
	for k := range rl.hits {
		if strings.HasPrefix(k, "old") {
			t.Fatalf("stale key %q was not evicted", k)
		}
	}
}

func TestRateLimiterDropsEmptyKey(t *testing.T) {
	rl := newRateLimiter(10, 10*time.Millisecond)
	rl.allow("a")
	time.Sleep(20 * time.Millisecond)
	if rl.exceeded("a") {
		t.Fatal("a is not over its limit")
	}
	if n := rl.size(); n != 0 {
		t.Fatalf("limiter holds %d keys after the only key went stale, want 0", n)
	}
}

// F89: a burst of concurrent wrong guesses from many IPs cannot get more
// comparisons than the global cap: each attempt reserves its slot before
// the secret is compared.
func TestFailedLoginCapHoldsUnderConcurrency(t *testing.T) {
	srv, ts := newTestHTTPServer(t)
	defer ts.Close()
	srv.loginLimiter = newRateLimiter(100000, time.Minute)
	srv.failedLoginLimiter = newRateLimiter(20, time.Minute)
	cid := registerTestClient(t, ts)
	// Hold every attempt that got this far, so all of a burst would be
	// in flight at once if the slot were taken only after the comparison.
	beforeSecretCompare = func() { time.Sleep(20 * time.Millisecond) }
	t.Cleanup(func() { beforeSecretCompare = func() {} })

	for run := range 3 {
		srv.failedLoginLimiter = newRateLimiter(20, time.Minute)
		var compared atomic.Int32
		var wg sync.WaitGroup
		sum := sha256.Sum256([]byte("verifier"))
		form := url.Values{
			"client_id": {cid}, "redirect_uri": {"https://c.example.com/cb"}, "state": {"s"},
			"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"},
			"response_type": {"code"}, "secret": {"wrong"},
		}.Encode()
		for i := range 500 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				// The handler directly, so the burst is not limited by a
				// listener's connection backlog.
				req := httptest.NewRequest(http.MethodPost, "/authorize", strings.NewReader(form))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.Header.Set("CF-Connecting-IP", fmt.Sprintf("10.%d.%d.%d", run, i/256, i%256))
				rec := httptest.NewRecorder()
				srv.handleAuthorize(rec, req)
				if rec.Code == http.StatusUnauthorized {
					compared.Add(1)
				}
			}()
		}
		wg.Wait()
		if got := compared.Load(); got > 20 {
			t.Fatalf("run %d: the secret was compared %d times, cap is 20", run, got)
		}
	}
}
