package main

type TorrentInfo struct {
	ID          int     `json:"id"`
	Name        string  `json:"name"`
	PercentDone float64 `json:"percentDone"`
	Category    string  `json:"category"`
	TotalSize   int64   `json:"totalSize"`
	AddedDate   int     `json:"addedDate"`
}

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
