package main

import (
	"regexp"
	"strconv"
)

type ListCommand struct {
}

type RemoveTorrentCommand struct {
	TorrentID int
}

func ParseCommand(cmdText string) (ok bool, cmd interface{}) {
	re_list_cmd := regexp.MustCompile("^/list\\s*?$")
	re_rem_cmd := regexp.MustCompile("^/remove\\s(\\d+)\\s*?$")

	if matched := re_list_cmd.Match([]byte(cmdText)); matched {
		return true, ListCommand{}
	}

	if found := re_rem_cmd.FindStringSubmatch(cmdText); len(found) == 2 {
		torrentID, err := strconv.Atoi(found[1])
		if err != nil {
			return false, nil
		}

		return true, RemoveTorrentCommand{TorrentID: torrentID}
	}
	return false, nil
}
