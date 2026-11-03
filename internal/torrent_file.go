package internal

import "errors"

// ErrNotTorrentFile is returned when data that should be a .torrent file is
// not one, typically an HTML page rutracker served instead (an expired
// session, a removed topic, an error page).
var ErrNotTorrentFile = errors.New("not a .torrent file")

// CheckTorrentFile returns ErrNotTorrentFile unless data looks like a
// .torrent file: a bencoded dictionary, which starts with 'd'. It catches
// HTML and empty bodies before they reach Transmission, which would only
// answer "invalid or corrupt torrent file".
func CheckTorrentFile(data []byte) error {
	if len(data) == 0 || data[0] != 'd' {
		return ErrNotTorrentFile
	}
	return nil
}
