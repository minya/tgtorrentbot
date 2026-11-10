// Package torrentlabels defines the Transmission label layout every binary
// writes and reads:
//
//	Labels[0] chat ID: the Telegram chat that gets the completion notification
//	Labels[1] category
//	Labels[2] user ID: the Telegram user who added the torrent
//
// The chat ID and the user ID are equal in a private chat with the bot, but a
// download started from a group has the negative group ID as its chat ID.
// The user ID only records who added a torrent: downloads are shared, so no
// access decision is made on it. Torrents labelled before Labels[2] existed
// carry only the first two.
package torrentlabels

import (
	"strconv"

	"github.com/minya/tgtorrentbot/internal/category"
)

// New returns the labels for a torrent added by userID from chatID.
func New(chatID, userID int64, category string) []string {
	return []string{strconv.FormatInt(chatID, 10), category, strconv.FormatInt(userID, 10)}
}

// Category returns the category label of a torrent, category.Others when
// it is missing or empty (torrents labelled by hand, or whose labels were
// cleared in Transmission). A label that is not a known category, e.g. one
// removed since, is returned as is: the torrent stays listed under it.
// Every binary reads the category through this, so a torrent shows the
// same category in the bot, the webapp and the MCP server.
func Category(labels []string) category.Category {
	if len(labels) >= 2 && labels[1] != "" {
		return category.Category(labels[1])
	}
	return category.Others
}
