package main

type Settings struct {
	BotToken        string                 `json:"botToken"`
	TransmissionRPC TransmissionRPCSettngs `json:"transmissionRPC"`
	DownloadPath    string                 `json:"downloadPath"`
}

type TransmissionRPCSettngs struct {
	Address  string `json:"address"`
	User     string `json:"user"`
	Password string `json:"password"`
}
