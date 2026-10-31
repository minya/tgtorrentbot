package commands

import "github.com/minya/tgtorrentbot/internal/category"

// Category represents a torrent download category. The list itself lives in
// internal/category, the single source of truth shared with the webapp and
// the MCP server.
type Category = category.Category

const (
	CategoryMovies      = category.Movies
	CategoryShows       = category.Shows
	CategoryMusic       = category.Music
	CategoryMusicVideos = category.MusicVideos
	CategoryAudiobooks  = category.Audiobooks
	CategoryOthers      = category.Others
)

// AllCategories returns all available categories
func AllCategories() []Category {
	return category.All()
}

// IsValidCategory checks if a category string is valid
func IsValidCategory(s string) bool {
	return category.IsValid(s)
}

// ParseCategory converts a string to a Category
func ParseCategory(s string) (Category, bool) {
	return category.Parse(s)
}
