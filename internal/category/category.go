// Package category is the single source of truth for download categories.
//
// Every binary (bot, webapp, MCP server) and the webapp frontend derive their
// category lists from All(). It has no dependencies so that any package can
// import it without pulling in Telegram/Rutracker/Transmission clients.
package category

// Category represents a torrent download category. Its string value is also
// the name of the download subdirectory: {DownloadPath}/{category}/.
type Category string

const (
	Movies      Category = "movies"
	Shows       Category = "shows"
	Music       Category = "music"
	MusicVideos Category = "musicvideos"
	Audiobooks  Category = "audiobooks"
	Others      Category = "others"
)

// All returns every download category, in display order.
func All() []Category {
	return []Category{Movies, Shows, Music, MusicVideos, Audiobooks, Others}
}

// Names returns the string values of All(), in the same order.
func Names() []string {
	all := All()
	names := make([]string, len(all))
	for i, c := range all {
		names[i] = string(c)
	}
	return names
}

// DisplayName returns the human-readable name for the category.
func (c Category) DisplayName() string {
	switch c {
	case Movies:
		return "Movies"
	case Shows:
		return "TV Shows"
	case Music:
		return "Music"
	case MusicVideos:
		return "Music Videos"
	case Audiobooks:
		return "Audiobooks"
	case Others:
		return "Other"
	default:
		return string(c)
	}
}

// String returns the string representation of the category.
func (c Category) String() string {
	return string(c)
}

// IsValid reports whether s is one of All().
func IsValid(s string) bool {
	for _, c := range All() {
		if string(c) == s {
			return true
		}
	}
	return false
}

// Parse converts a string to a Category, reporting whether it is valid.
func Parse(s string) (Category, bool) {
	if IsValid(s) {
		return Category(s), true
	}
	return "", false
}
