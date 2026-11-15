// Package completion detects torrents that finished downloading between two
// polls of Transmission, for the bot's completion checker, which notifies
// the chat and hands the torrent to the auto-fixer.
package completion

import "github.com/odwrtw/transmission"

// NewlyCompleted returns the torrents of current that completed since the
// previous check. A torrent seen before completes when its PercentDone goes
// from below 1 to 1. A torrent not seen before (first check, added from the
// webapp or outside the bot, or added and completed between two checks)
// counts when Transmission's doneDate is at or after since, so torrents that
// were already complete before the checker started are not reported.
func NewlyCompleted(previous, current transmission.TorrentMap, since int64) []*transmission.Torrent {
	var done []*transmission.Torrent
	for hash, torrent := range current {
		if torrent.PercentDone < 1 {
			continue
		}
		if prev, ok := previous[hash]; ok {
			if prev.PercentDone < 1 {
				done = append(done, torrent)
			}
			continue
		}
		if torrent.DoneDate > 0 && int64(torrent.DoneDate) >= since {
			done = append(done, torrent)
		}
	}
	return done
}
