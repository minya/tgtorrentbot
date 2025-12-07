package commands

// Category represents a torrent download category
type Category string

const (
	CategoryMovies     Category = "movies"
	CategoryShows      Category = "shows"
	CategoryMusic      Category = "music"
	CategoryAudiobooks Category = "audiobooks"
	CategoryOthers     Category = "others"
)

// AllCategories returns all available categories
func AllCategories() []Category {
	return []Category{
		CategoryMovies,
		CategoryShows,
		CategoryMusic,
		CategoryAudiobooks,
		CategoryOthers,
	}
}

// DisplayName returns the human-readable name for the category
func (c Category) DisplayName() string {
	switch c {
	case CategoryMovies:
		return "Фильмы"
	case CategoryShows:
		return "Сериалы"
	case CategoryMusic:
		return "Музыка"
	case CategoryAudiobooks:
		return "Аудиокниги"
	case CategoryOthers:
		return "Другое"
	default:
		return string(c)
	}
}

// String returns the string representation of the category
func (c Category) String() string {
	return string(c)
}

// IsValid checks if a category string is valid
func IsValidCategory(s string) bool {
	for _, cat := range AllCategories() {
		if string(cat) == s {
			return true
		}
	}
	return false
}

// ParseCategory converts a string to a Category
func ParseCategory(s string) (Category, bool) {
	if IsValidCategory(s) {
		return Category(s), true
	}
	return "", false
}
