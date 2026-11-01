package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func clearTGTEnv(t *testing.T) {
	t.Helper()
	for _, k := range requiredEnvVars {
		t.Setenv(k, "")
	}
}

func writeSettings(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

const fullSettingsJSON = `{
	"botToken": "tok", "webHookURL": "https://h/hook", "downloadPath": "/d",
	"transmissionRPC": {"address": "http://t", "user": "u", "password": "p"},
	"rutrackerConfig": {"username": "ru", "password": "rp"},
	"allowedUsers": [1]
}`

// F16: a settings file missing required values must fail, not start the bot
// with an empty webhook URL (which deregisters the webhook).
func TestSettingsFileRequiresEveryField(t *testing.T) {
	clearTGTEnv(t)
	_, err := ReadSettings(writeSettings(t, `{"allowedUsers":[1]}`))
	if err == nil {
		t.Fatal("ReadSettings accepted a file with only allowedUsers")
	}
	for _, want := range []string{"botToken", "webHookURL", "downloadPath", "transmissionRPC.address", "rutrackerConfig.password"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %s", err, want)
		}
	}
}

func TestSettingsFileComplete(t *testing.T) {
	clearTGTEnv(t)
	s, err := ReadSettings(writeSettings(t, fullSettingsJSON))
	if err != nil {
		t.Fatal(err)
	}
	if s.WebHookURL != "https://h/hook" || len(s.AllowedUsers) != 1 {
		t.Fatalf("settings = %+v", s)
	}
}

func TestSettingsEnvStillValidated(t *testing.T) {
	clearTGTEnv(t)
	t.Setenv("TGT_BOTTOKEN", "tok")
	_, err := ReadSettings(filepath.Join(t.TempDir(), "absent.json"))
	if err == nil || !strings.Contains(err.Error(), "TGT_WEBHOOKURL") || !strings.Contains(err.Error(), "TGT_ALLOWED_USERS") {
		t.Fatalf("err = %v, want missing TGT_WEBHOOKURL and TGT_ALLOWED_USERS", err)
	}
}
