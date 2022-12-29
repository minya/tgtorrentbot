package main

import (
	"regexp"
	"strconv"
	"strings"
)

type ListCommand struct {
}

type RemoveTorrentCommand struct {
	TorrentID int
}
type SearchCommand struct {
	Pattern string
}
type DownloadCommand struct {
	URL string
}

func ParseCommand(cmdText string) (ok bool, cmd interface{}) {
	re_list_cmd := regexp.MustCompile("^/list\\s*?$")
	re_rem_cmd := regexp.MustCompile("^/remove\\s(\\d+)\\s*?$")
	re_search_cmd := regexp.MustCompile("^/search\\s(.+?)$")
	re_dl_cmd := regexp.MustCompile("^/dl\\s(.+?)$")

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

	if found := re_search_cmd.FindStringSubmatch(cmdText); len(found) == 2 {
		pattern := strings.TrimSpace(found[1])
		return true, SearchCommand{Pattern: pattern}
	}

	if found := re_dl_cmd.FindStringSubmatch(cmdText); len(found) == 2 {
		url := strings.TrimSpace(found[1])
		return true, DownloadCommand{URL: url}
	}

	return false, nil
}
