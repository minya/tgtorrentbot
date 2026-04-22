package main

type DownloadRequest struct {
	DownloadURL string `json:"downloadUrl"`
	Category    string `json:"category"`
}

type SearchResult struct {
	Title       string `json:"title"`
	Size        string `json:"size"`
	Seeders     int    `json:"seeders"`
	DownloadURL string `json:"downloadUrl"`
}

// WSMessage is a WebSocket message envelope.
type WSMessage struct {
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

// TorrentUpdate contains progress data for an active torrent, sent over WebSocket.
type TorrentUpdate struct {
	ID               int     `json:"id"`
	Name             string  `json:"name"`
	PercentDone      float64 `json:"percentDone"`
	Category         string  `json:"category"`
	RateDownload     int     `json:"rateDownload"`
	Eta              int     `json:"eta"`
	PeersConnected   int     `json:"peersConnected"`
	PeersSendingToUs int     `json:"peersSendingToUs"`
	TotalSize        int64   `json:"totalSize"`
	IsComplete       bool    `json:"isComplete"`
}
