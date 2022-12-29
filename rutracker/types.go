package rutracker

type ItemSize struct {
	Size float64
	Unit string
}
type RutrackerSearchItem struct {
	TopicID     int
	URL         string
	DownloadURL string
	Title       string
	Seeders     int
	Size        ItemSize
}
