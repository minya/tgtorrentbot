package main

import (
	"os"
	"path/filepath"
	"testing"
)

func setEnvVars(t *testing.T, vars map[string]string) {
	t.Helper()
	for k, v := range vars {
		t.Setenv(k, v)
	}
}

func TestLoadConfig_AllFields(t *testing.T) {
	setEnvVars(t, map[string]string{
		"TGT_BOTTOKEN":            "test-bot-token",
		"TGT_RPC_ADDR":            "http://localhost:9091/rpc",
		"TGT_RPC_USER":            "admin",
		"TGT_RPC_PASSWORD":        "secret",
		"TGT_RUTRACKER_USERNAME":  "ru_user",
		"TGT_RUTRACKER_PASSWORD":  "ru_pass",
		"TGT_DOWNLOADPATH":        "/downloads",
		"TGT_LOGLEVEL":            "debug",
		"TGT_JELLYFIN_URL":        "http://jellyfin:8096",
		"TGT_JELLYFIN_API_KEY":    "jf-api-key-123",
		"TGT_INCOMPLETE_PATH":     "/downloads/incomplete",
	})

	cfg := loadConfig()

	if cfg.BotToken != "test-bot-token" {
		t.Errorf("BotToken = %q, want %q", cfg.BotToken, "test-bot-token")
	}
	if cfg.TransmissionAddr != "http://localhost:9091/rpc" {
		t.Errorf("TransmissionAddr = %q, want %q", cfg.TransmissionAddr, "http://localhost:9091/rpc")
	}
	if cfg.DownloadPath != "/downloads" {
		t.Errorf("DownloadPath = %q, want %q", cfg.DownloadPath, "/downloads")
	}
	if cfg.JellyfinURL != "http://jellyfin:8096" {
		t.Errorf("JellyfinURL = %q, want %q", cfg.JellyfinURL, "http://jellyfin:8096")
	}
	if cfg.JellyfinAPIKey != "jf-api-key-123" {
		t.Errorf("JellyfinAPIKey = %q, want %q", cfg.JellyfinAPIKey, "jf-api-key-123")
	}
	if cfg.IncompletePath != "/downloads/incomplete" {
		t.Errorf("IncompletePath = %q, want %q", cfg.IncompletePath, "/downloads/incomplete")
	}
}

func TestLoadConfig_IncompletePathDefault(t *testing.T) {
	setEnvVars(t, map[string]string{
		"TGT_DOWNLOADPATH": "/downloads/complete",
	})
	// Ensure TGT_INCOMPLETE_PATH is not set
	os.Unsetenv("TGT_INCOMPLETE_PATH")

	cfg := loadConfig()

	expected := filepath.Join("/downloads/complete", "..", "incomplete")
	if cfg.IncompletePath != expected {
		t.Errorf("IncompletePath = %q, want %q", cfg.IncompletePath, expected)
	}
}

func TestLoadConfig_IncompletePathEmptyWhenNoDownloadPath(t *testing.T) {
	os.Unsetenv("TGT_DOWNLOADPATH")
	os.Unsetenv("TGT_INCOMPLETE_PATH")

	cfg := loadConfig()

	if cfg.IncompletePath != "" {
		t.Errorf("IncompletePath = %q, want empty", cfg.IncompletePath)
	}
}

func TestLoadConfig_JellyfinOptional(t *testing.T) {
	setEnvVars(t, map[string]string{
		"TGT_DOWNLOADPATH": "/downloads",
	})
	os.Unsetenv("TGT_JELLYFIN_URL")
	os.Unsetenv("TGT_JELLYFIN_API_KEY")

	cfg := loadConfig()

	if cfg.JellyfinURL != "" {
		t.Errorf("JellyfinURL = %q, want empty", cfg.JellyfinURL)
	}
	if cfg.JellyfinAPIKey != "" {
		t.Errorf("JellyfinAPIKey = %q, want empty", cfg.JellyfinAPIKey)
	}
}

func TestLoadConfig_BotTokenTrimmed(t *testing.T) {
	setEnvVars(t, map[string]string{
		"TGT_BOTTOKEN": "  token-with-spaces  ",
	})

	cfg := loadConfig()

	if cfg.BotToken != "token-with-spaces" {
		t.Errorf("BotToken = %q, want %q", cfg.BotToken, "token-with-spaces")
	}
}
