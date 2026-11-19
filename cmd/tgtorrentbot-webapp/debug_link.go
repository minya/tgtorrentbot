package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strings"
	"time"
)

const debugLinkUsage = `usage: tgtorrentbot-webapp debug-link [-user ID] [-valid DURATION] [BASE_URL]

Prints a link that opens the webapp in a normal browser, logged in as an
allowed user for -valid (default 1h, at most 24h). It signs Telegram init
data with TGT_BOTTOKEN, so the server accepts it unchanged; the data travels
in the URL fragment
(#tgWebAppData=...), which telegram-web-app.js reads and the browser never
sends to the server. The link cannot be revoked short of changing the bot
token, so keep -valid short. Reads TGT_BOTTOKEN and TGT_ALLOWED_USERS, e.g.:

  docker exec tgt-webapp /app/tgtorrentbot-webapp debug-link https://webapp.example.com
`

// runDebugLink is the debug-link subcommand. The link goes to stdout, the
// warning to stderr, so the link can be piped or copied alone.
func runDebugLink(args []string, getenv func(string) string, now time.Time, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("debug-link", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, debugLinkUsage) }
	userID := fs.Int64("user", 0, "Telegram user ID to log in as (default: the first of TGT_ALLOWED_USERS)")
	valid := fs.Duration("valid", time.Hour, "how long the link logs in, at most 24h")
	// flag stops at the first non-flag argument; parse on after each one, so
	// -user may come before or after BASE_URL.
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
		if fs.NArg() == 0 {
			break
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
	if len(positional) > 1 {
		fs.Usage()
		return fmt.Errorf("at most one BASE_URL, got %d: %s", len(positional), strings.Join(positional, " "))
	}

	if *valid <= 0 || *valid > initDataMaxAge {
		return fmt.Errorf("-valid must be between 0 and %s, got %s", initDataMaxAge, *valid)
	}

	botToken := strings.TrimSpace(getenv("TGT_BOTTOKEN"))
	if botToken == "" {
		return errors.New("TGT_BOTTOKEN is not set")
	}
	allowed, err := parseAllowedUsers(getenv("TGT_ALLOWED_USERS"))
	if err != nil {
		return fmt.Errorf("TGT_ALLOWED_USERS: %w", err)
	}
	if len(allowed) == 0 {
		return errors.New("TGT_ALLOWED_USERS is not set")
	}
	if *userID == 0 {
		*userID = allowed[0]
	}
	// The server would refuse it: authenticateInitData checks the list.
	if !slices.Contains(allowed, *userID) {
		return fmt.Errorf("user %d is not in TGT_ALLOWED_USERS", *userID)
	}

	// The server accepts init data for initDataMaxAge after auth_date, so a
	// backdated auth_date ends the login after -valid.
	authDate := now.Add(*valid - initDataMaxAge)
	link := "#tgWebAppData=" + url.QueryEscape(signInitData(botToken, *userID, authDate))
	if len(positional) == 1 {
		link = strings.TrimRight(positional[0], "/#") + "/" + link
	}
	fmt.Fprintln(stdout, link)
	fmt.Fprintf(stderr, "This link logs in as user %d until %s. Do not share it.\n",
		*userID, now.Add(*valid).Format(time.RFC3339))
	return nil
}
