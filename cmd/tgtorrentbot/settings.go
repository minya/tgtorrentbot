package main

import (
	"github.com/minya/rutracker"
)

type Settings struct {
	BotToken        string                  `json:"botToken"`
	WebHookURL      string                  `json:"webHookURL"`
	DownloadPath    string                  `json:"downloadPath"`
	TransmissionRPC TransmissionRPCSettings `json:"transmissionRPC"`
	RutrackerConfig rutracker.Config        `json:"rutrackerConfig"`
	LogLevel        string                  `json:"logLevel"`
	WebAppURL       string                  `json:"webAppURL"`
	AllowedUsers    []int64                 `json:"allowedUsers"`
	// WebhookSecret is the secret_token Telegram sends with every webhook
	// request. Optional: derived from BotToken when empty.
	WebhookSecret string `json:"webhookSecret"`
	// Autofix configures the auto-fixer. Optional: it runs when
	// AnthropicAPIKey is set.
	Autofix AutofixSettings `json:"autofix"`
}

// AutofixSettings configures the auto-fixer: Claude inspects every completed
// download and fixes its tags and folder name with the media tools. It needs
// the downloads mounted at DownloadPath; Jellyfin and Audiobookshelf are
// optional (their rescans fail without them).
type AutofixSettings struct {
	AnthropicAPIKey string `json:"anthropicApiKey"`
	// Model defaults to autofix.DefaultModel.
	Model string `json:"model"`
	// IncompletePath defaults to {DownloadPath}/incomplete.
	IncompletePath          string `json:"incompletePath"`
	JellyfinURL             string `json:"jellyfinURL"`
	JellyfinAPIKey          string `json:"jellyfinApiKey"`
	AudiobookshelfURL       string `json:"audiobookshelfURL"`
	AudiobookshelfAPIKey    string `json:"audiobookshelfApiKey"`
	AudiobookshelfLibraryID string `json:"audiobookshelfLibraryId"`
}

type TransmissionRPCSettings struct {
	Address  string `json:"address"`
	User     string `json:"user"`
	Password string `json:"password"`
}
