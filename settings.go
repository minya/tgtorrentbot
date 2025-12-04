package main

import (
	"github.com/minya/tgtorrentbot/rutracker"
)

type Settings struct {
	BotToken        string                 `json:"botToken"`
	DownloadPath    string                 `json:"downloadPath"`
	TransmissionRPC TransmissionRPCSettngs `json:"transmissionRPC"`
	RutrackerConfig rutracker.Config       `json:"rutrackerConfig"`
	LogLevel        string                 `json:"logLevel"`
}

type TransmissionRPCSettngs struct {
	Address  string `json:"address"`
	User     string `json:"user"`
	Password string `json:"password"`
}
