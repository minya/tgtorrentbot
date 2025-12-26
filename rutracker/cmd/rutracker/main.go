package main

import (
	"encoding/json"
	"flag"
	"fmt"

	"github.com/minya/tgtorrentbot/rutracker"
)

func main() {
	var username, passwd, pattern string
	readArgs(&username, &passwd, &pattern)
	client, err := rutracker.NewAuthenticatedRutrackerClient(username, passwd)
	if err != nil {
		panic(err)
	}
	results, err := client.Find(pattern)
	if err != nil {
		panic(err)
	}

	jsonBytes, err := json.Marshal(results)

	if err != nil {
		panic(err)
	}
	fmt.Println(string(jsonBytes))
}

func readArgs(username *string, passwd *string, pattern *string) {
	flag.StringVar(username, "u", "", "Username")
	flag.StringVar(passwd, "p", "", "Password")
	flag.StringVar(pattern, "s", "", "Pattern to search for")
	flag.Parse()
}
