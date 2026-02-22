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
}

type TransmissionRPCSettings struct {
	Address  string `json:"address"`
	User     string `json:"user"`
	Password string `json:"password"`
}
