package rutracker

import (
	"regexp"
	"strconv"

	"github.com/minya/logger"
	"golang.org/x/text/encoding/charmap"
)

func ParseSearchItems(responseBytes *[]byte) ([]RutrackerSearchItem, error) {
	re := regexp.MustCompile("<a.+?href=\"(viewtopic\\.php\\?t=(\\d+))\">(.+?)<\\/a>[\\s\\S]+?<a.+?href=\"(dl.php\\?t=\\d+)\">([\\d\\.]+)&nbsp;(\\w+)\\s\\&.+?<\\/a>[\\s\\S]+?<b class=\"seedmed\">(\\d+)<\\/b>")
	out, err := charmap.Windows1251.NewDecoder().Bytes(*responseBytes)
	if err != nil {
		logger.Error(err, "cannot decode response bytes")
		return nil, err
	}
	searchResultsHtml := string(out)

	match := re.FindAllStringSubmatch(searchResultsHtml, -1)

	var result []RutrackerSearchItem

	for i := range match {
		group := match[i]
		topicID, err := strconv.Atoi(group[2])
		if err != nil {
			continue
		}
		seeders, err := strconv.Atoi(group[7])
		if err != nil {
			logger.Error(err, "Cannot parse seeders. Got %s", group[7])
			continue
		}
		item := RutrackerSearchItem{
			TopicID:     topicID,
			DownloadURL: group[4],
			URL:         group[1],
			Title:       group[3],
			Seeders:     seeders,
			Size: ItemSize{
				Size: parseSize(group[5]),
				Unit: group[6],
			},
		}
		result = append(result, item)
	}
	return result, nil
}

func parseSize(sizeStr string) float64 {
	size, err := strconv.ParseFloat(sizeStr, 64)
	if err != nil {
		logger.Error(err, "Cannot parse size. Got %s", sizeStr)
		return 0
	}
	return size
}
