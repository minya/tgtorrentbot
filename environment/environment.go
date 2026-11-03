package environment

import (
	"github.com/minya/rutracker"
	"github.com/minya/telegram"
	"github.com/minya/tgtorrentbot/internal"
	"github.com/minya/tgtorrentbot/internal/rutrackersession"
	"github.com/odwrtw/transmission"
)

type Env struct {
	TransmissionClient *transmission.Client
	TgApi              *telegram.API
	DownloadPath       string
	RutrackerConfig    *rutracker.Config
	// Rutracker is the one logged-in rutracker client of the process; use
	// it rather than logging in per request.
	Rutracker    *rutrackersession.Session
	WebAppURL    string
	AllowedUsers []int64
	FileIDLookup *internal.FileIDLookup
}

func Environment(
	transmissionClient *transmission.Client,
	tgApi *telegram.API,
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
