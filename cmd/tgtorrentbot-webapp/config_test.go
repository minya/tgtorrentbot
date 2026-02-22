package main

import (
	"testing"
)

func setEnvVars(t *testing.T, vars map[string]string) {
	t.Helper()
	for k, v := range vars {
		t.Setenv(k, v)
	}
}

// setRequiredEnvVars sets all required env vars to valid defaults.
// Individual tests can override specific vars after calling this.
func setRequiredEnvVars(t *testing.T) {
	t.Helper()
	setEnvVars(t, map[string]string{
		"TGT_BOTTOKEN":       "test-token",
		"TGT_DOWNLOADPATH":   "/downloads",
		"TGT_ALLOWED_USERS":  "111",
	})
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
		"TGT_ALLOWED_USERS":       "123,456",
	})

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}

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
	if len(cfg.AllowedUsers) != 2 || cfg.AllowedUsers[0] != 123 || cfg.AllowedUsers[1] != 456 {
		t.Errorf("AllowedUsers = %v, want [123 456]", cfg.AllowedUsers)
	}
}

func TestLoadConfig_IncompletePathDefault(t *testing.T) {
	setRequiredEnvVars(t)
	setEnvVars(t, map[string]string{
		"TGT_DOWNLOADPATH":    "/downloads",
		"TGT_INCOMPLETE_PATH": "",
	})

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}

	expected := "/downloads/incomplete"
	if cfg.IncompletePath != expected {
		t.Errorf("IncompletePath = %q, want %q", cfg.IncompletePath, expected)
	}
}

func TestLoadConfig_IncompletePathEmptyWhenNoDownloadPath(t *testing.T) {
	setRequiredEnvVars(t)
	setEnvVars(t, map[string]string{
		"TGT_DOWNLOADPATH":    "",
		"TGT_INCOMPLETE_PATH": "",
	})

	// Missing TGT_DOWNLOADPATH is a required field, expect error
	_, err := loadConfig()
	if err == nil {
		t.Fatal("loadConfig() should return error when TGT_DOWNLOADPATH is empty")
	}
}

func TestLoadConfig_JellyfinOptional(t *testing.T) {
	setRequiredEnvVars(t)
	setEnvVars(t, map[string]string{
		"TGT_JELLYFIN_URL":    "",
		"TGT_JELLYFIN_API_KEY": "",
	})

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}

	if cfg.JellyfinURL != "" {
		t.Errorf("JellyfinURL = %q, want empty", cfg.JellyfinURL)
	}
	if cfg.JellyfinAPIKey != "" {
		t.Errorf("JellyfinAPIKey = %q, want empty", cfg.JellyfinAPIKey)
	}
}

func TestLoadConfig_BotTokenTrimmed(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("TGT_BOTTOKEN", "  token-with-spaces  ")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}

	if cfg.BotToken != "token-with-spaces" {
		t.Errorf("BotToken = %q, want %q", cfg.BotToken, "token-with-spaces")
	}
}

func TestLoadConfig_MissingRequired(t *testing.T) {
	// No env vars set at all
	_, err := loadConfig()
	if err == nil {
		t.Fatal("loadConfig() should return error when required vars are missing")
	}
}

func TestLoadConfig_InvalidAllowedUsers(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("TGT_ALLOWED_USERS", "alice,bob")

	_, err := loadConfig()
	if err == nil {
		t.Fatal("loadConfig() should return error for invalid TGT_ALLOWED_USERS")
	}
}
