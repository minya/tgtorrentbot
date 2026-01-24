package environment

import (
	"github.com/minya/telegram"
	"github.com/minya/rutracker"
	"github.com/odwrtw/transmission"
)

type Env struct {
	TransmissionClient *transmission.Client
	TgApi              *telegram.Api
	DownloadPath       string
	RutrackerConfig    *rutracker.Config
	WebAppURL          string
}

func Environment(
	transmissionClient *transmission.Client,
	tgApi *telegram.Api,
	downloadPath string,
	rutrackerConfig *rutracker.Config,
) *Env {
	return &Env{
		TransmissionClient: transmissionClient,
		TgApi:              tgApi,
		DownloadPath:       downloadPath,
		RutrackerConfig:    rutrackerConfig,
	}
}
