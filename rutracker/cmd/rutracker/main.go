package main

import (
	"flag"
	"fmt"

	"github.com/minya/tgtorrentbot/rutracker"
)

func main() {
	var username, passwd string
	readArgs(&username, &passwd)
	client, err := rutracker.NewAuthenticatedRutrackerClient(username, passwd)
	if err != nil {
		panic(err)
	}
	results, err := client.Find("The Big Bang Theory")
	if err != nil {
		panic(err)
	}
	if len(results) == 0 {
		println("No results")
	}

	for _, result := range results {
		fmt.Printf("Title: %v\tSize: %v\tURL: %v\tSeeders: %v\n", result.Title, result.Size, result.URL, result.Seeders)
	}
}

func readArgs(username *string, passwd *string) {
	flag.StringVar(username, "u", "", "Username")
	flag.StringVar(passwd, "p", "", "Password")
	flag.Parse()
}
