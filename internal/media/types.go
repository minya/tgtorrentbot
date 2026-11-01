package media

// TorrentInfo is the minimum set of torrent fields needed by MergeItems.
// It's used both as a merge input and as an API response payload.
type TorrentInfo struct {
	ID               int     `json:"id"`
	Name             string  `json:"name"`
	PercentDone      float64 `json:"percentDone"`
	Category         string  `json:"category"`
	TotalSize        int64   `json:"totalSize"`
	AddedDate        int     `json:"addedDate"`
	RateDownload     int     `json:"rateDownload"`
	Eta              int     `json:"eta"`
	PeersConnected   int     `json:"peersConnected"`
	PeersSendingToUs int     `json:"peersSendingToUs"`
}

// UnifiedItem represents a media item merged from multiple sources.
type UnifiedItem struct {
	Name             string   `json:"name"`
	Category         string   `json:"category"`
	Sources          []string `json:"sources"`
	TorrentID        *int     `json:"torrentId,omitempty"`
	PercentDone      *float64 `json:"percentDone,omitempty"`
	TotalSize        int64    `json:"totalSize"`
	AddedDate        *int     `json:"addedDate,omitempty"`
	ModTime          *int64   `json:"mtime,omitempty"` // newest filesystem mtime, unix seconds
	IsIncomplete     bool     `json:"isIncomplete,omitempty"`
	RateDownload     *int     `json:"rateDownload,omitempty"`
	Eta              *int     `json:"eta,omitempty"`
	PeersConnected   *int     `json:"peersConnected,omitempty"`
	PeersSendingToUs *int     `json:"peersSendingToUs,omitempty"`
	// Paths lists where the item's data is on disk, as found by the
	// filesystem scan: the completed {DownloadPath}/{category}/{name} first,
	// then {IncompletePath}/{name} while still downloading or orphaned.
	// Empty for items with no filesystem source.
	Paths []string `json:"-"`
}
