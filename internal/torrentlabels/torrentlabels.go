// Package torrentlabels defines the Transmission label layout every binary
// writes and reads:
//
//	Labels[0] chat ID: the Telegram chat that gets the completion notification
//	Labels[1] category
//	Labels[2] user ID: the Telegram user who added the torrent (its owner)
//
// The chat ID and the user ID are equal in a private chat with the bot, but a
// download started from a group has the negative group ID as its chat ID.
// Torrents labelled before Labels[2] existed carry only the first two; for
// them Owner falls back to Labels[0], which was the user ID for every
// private-chat and webapp download.
package torrentlabels

import "strconv"

// New returns the labels for a torrent added by userID from chatID.
func New(chatID, userID int64, category string) []string {
	return []string{strconv.FormatInt(chatID, 10), category, strconv.FormatInt(userID, 10)}
}

// Owner returns the user ID that owns a torrent with these labels, or false
// when the labels carry none.
func Owner(labels []string) (int64, bool) {
	s := ""
	switch {
	case len(labels) >= 3:
		s = labels[2]
	case len(labels) >= 1:
		s = labels[0]
	default:
		return 0, false
	}
	id, err := strconv.ParseInt(s, 10, 64)
	return id, err == nil
}

// OwnedBy reports whether a torrent with these labels belongs to userID.
func OwnedBy(labels []string, userID int64) bool {
	owner, ok := Owner(labels)
	return ok && owner == userID
}
