package main

import (
	"bytes"
	"net/url"
	"strings"
	"testing"
	"time"
)

const testBotToken = "123456:test-token"

// signInitData makes data that the server's own check accepts, so
// debug-link and validate stay in step.
func TestSignedInitDataAuthenticates(t *testing.T) {
	app := &App{config: Config{BotToken: testBotToken, AllowedUsers: []int64{42}}}
	raw := signInitData(testBotToken, 42, time.Now())

	userID, err := app.authenticateInitData(raw)
	if err != nil {
		t.Fatalf("authenticateInitData: %v", err)
	}
	if userID != 42 {
		t.Errorf("userID = %d, want 42", userID)
	}
}

func TestSignedInitDataRefused(t *testing.T) {
	app := &App{config: Config{BotToken: testBotToken, AllowedUsers: []int64{42}}}
	cases := map[string]string{
		"other bot token":  signInitData("654321:other", 42, time.Now()),
		"user not allowed": signInitData(testBotToken, 7, time.Now()),
		"older than 24h":   signInitData(testBotToken, 42, time.Now().Add(-25*time.Hour)),
		"user id changed":  strings.Replace(signInitData(testBotToken, 42, time.Now()), "%22id%22%3A42", "%22id%22%3A43", 1),
	}
	for name, raw := range cases {
		if _, err := app.authenticateInitData(raw); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func debugLinkEnv(env map[string]string) func(string) string {
	return func(k string) string { return env[k] }
}

func TestDebugLinkValidates(t *testing.T) {
	env := debugLinkEnv(map[string]string{"TGT_BOTTOKEN": testBotToken, "TGT_ALLOWED_USERS": "42,99"})
	var out, errOut bytes.Buffer
	if err := runDebugLink([]string{"-user", "99", "https://webapp.example.com/"}, env, time.Now(), &out, &errOut); err != nil {
		t.Fatalf("runDebugLink: %v", err)
	}

	link := strings.TrimSpace(out.String())
	base, fragment, ok := strings.Cut(link, "/#tgWebAppData=")
	if !ok || base != "https://webapp.example.com" {
		t.Fatalf("link = %q, want https://webapp.example.com/#tgWebAppData=...", link)
	}
	// telegram-web-app.js URL-decodes the fragment value into tg.initData.
	raw, err := url.QueryUnescape(fragment)
	if err != nil {
		t.Fatal(err)
	}
	app := &App{config: Config{BotToken: testBotToken, AllowedUsers: []int64{42, 99}}}
	if userID, err := app.authenticateInitData(raw); err != nil || userID != 99 {
		t.Errorf("authenticateInitData = %d, %v; want 99", userID, err)
	}
	if !strings.Contains(errOut.String(), "Do not share") {
		t.Errorf("stderr lacks the warning: %q", errOut.String())
	}
}

func TestDebugLinkDefaultsToFirstAllowedUser(t *testing.T) {
	env := debugLinkEnv(map[string]string{"TGT_BOTTOKEN": testBotToken, "TGT_ALLOWED_USERS": "42,99"})
	var out bytes.Buffer
	if err := runDebugLink(nil, env, time.Now(), &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("runDebugLink: %v", err)
	}
	raw, _ := url.QueryUnescape(strings.TrimPrefix(strings.TrimSpace(out.String()), "#tgWebAppData="))
	d, err := newInitData(raw)
	if err != nil {
		t.Fatal(err)
	}
	if id, _ := d.userID(); id != 42 {
		t.Errorf("user = %d, want 42", id)
	}
}

func TestDebugLinkRefusesUserNotAllowed(t *testing.T) {
	env := debugLinkEnv(map[string]string{"TGT_BOTTOKEN": testBotToken, "TGT_ALLOWED_USERS": "42"})
	var out bytes.Buffer
	if err := runDebugLink([]string{"-user", "7"}, env, time.Now(), &out, &bytes.Buffer{}); err == nil {
		t.Error("user 7 accepted")
	}
	if out.Len() != 0 {
		t.Errorf("printed a link: %q", out.String())
	}
}

// flag stops at the first non-flag argument; -user after BASE_URL must still
// count.
func TestDebugLinkFlagAfterURL(t *testing.T) {
	env := debugLinkEnv(map[string]string{"TGT_BOTTOKEN": testBotToken, "TGT_ALLOWED_USERS": "42,99"})
	var out bytes.Buffer
	if err := runDebugLink([]string{"https://webapp.example.com", "-user", "99"}, env, time.Now(), &out, &bytes.Buffer{}); err != nil {
		t.Fatalf("runDebugLink: %v", err)
	}
	_, fragment, ok := strings.Cut(strings.TrimSpace(out.String()), "https://webapp.example.com/#tgWebAppData=")
	if !ok {
		t.Fatalf("link = %q", out.String())
	}
	raw, _ := url.QueryUnescape(fragment)
	d, err := newInitData(raw)
	if err != nil {
		t.Fatal(err)
	}
	if id, _ := d.userID(); id != 99 {
		t.Errorf("user = %d, want 99", id)
	}
}

func TestDebugLinkRefusesTwoURLs(t *testing.T) {
	env := debugLinkEnv(map[string]string{"TGT_BOTTOKEN": testBotToken, "TGT_ALLOWED_USERS": "42"})
	err := runDebugLink([]string{"https://a.example", "-user", "42", "https://b.example"}, env, time.Now(), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "got 2") {
		t.Errorf("err = %v, want at most one BASE_URL, got 2", err)
	}
}

// -valid backdates auth_date, so the server's 24 h limit ends the login
// after -valid.
func TestDebugLinkValidEndsLogin(t *testing.T) {
	env := debugLinkEnv(map[string]string{"TGT_BOTTOKEN": testBotToken, "TGT_ALLOWED_USERS": "42"})
	app := &App{config: Config{BotToken: testBotToken, AllowedUsers: []int64{42}}}
	login := func(signedAgo time.Duration, args ...string) error {
		var out bytes.Buffer
		if err := runDebugLink(args, env, time.Now().Add(-signedAgo), &out, &bytes.Buffer{}); err != nil {
			t.Fatalf("runDebugLink %v: %v", args, err)
		}
		raw, _ := url.QueryUnescape(strings.TrimPrefix(strings.TrimSpace(out.String()), "#tgWebAppData="))
		_, err := app.authenticateInitData(raw)
		return err
	}
	if err := login(59 * time.Minute); err != nil {
		t.Errorf("default 1h, 59 min later: %v", err)
	}
	if err := login(61 * time.Minute); err == nil {
		t.Error("default 1h, 61 min later: accepted")
	}
	if err := login(5*time.Hour, "-valid", "6h"); err != nil {
		t.Errorf("-valid 6h, 5 h later: %v", err)
	}
	if err := login(7*time.Hour, "-valid", "6h"); err == nil {
		t.Error("-valid 6h, 7 h later: accepted")
	}
}

func TestDebugLinkRefusesValidOutOfRange(t *testing.T) {
	env := debugLinkEnv(map[string]string{"TGT_BOTTOKEN": testBotToken, "TGT_ALLOWED_USERS": "42"})
	for _, v := range []string{"0s", "-1h", "25h"} {
		if err := runDebugLink([]string{"-valid", v}, env, time.Now(), &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
			t.Errorf("-valid %s accepted", v)
		}
	}
}

func TestDebugLinkNeedsToken(t *testing.T) {
	env := debugLinkEnv(map[string]string{"TGT_ALLOWED_USERS": "42"})
	if err := runDebugLink(nil, env, time.Now(), &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Error("ran without TGT_BOTTOKEN")
	}
}
