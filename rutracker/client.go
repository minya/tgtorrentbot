package rutracker

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/minya/goutils/web"
)

type RutrackerClient struct {
	username   string
	password   string
	httpClient *http.Client
}

func NewAuthenticatedRutrackerClient(username string, password string) (RutrackerClient, error) {
	httpClient := http.Client{
		Jar:       web.NewJar(),
		Transport: web.DefaultTransport(5000),
	}

	client := RutrackerClient{
		username:   username,
		password:   password,
		httpClient: &httpClient,
	}

	err := authenticate(&httpClient, username, password)
	if err != nil {
		return client, err
	}
	return client, nil
}

func authenticate(httpClient *http.Client, username string, password string) error {
	rutrackerLoginURL := "https://rutracker.org/forum/login.php"
	body := strings.NewReader("login_username=" + username + "&login_password=" + password + "&login=%C2%F5%EE%E4")
	res, err := httpClient.Post(rutrackerLoginURL, "application/x-www-form-urlencoded", body)
	if err != nil {
		return err
	}
	if res.StatusCode >= 400 {
		return fmt.Errorf("rutracker login failed. Status code: %v", res.StatusCode)
	}

	return nil
}

func (c *RutrackerClient) Find(pattern string) ([]RutrackerSearchItem, error) {
	searchURL := "https://rutracker.org/forum/tracker.php?nm=" + pattern
	searchBody := strings.NewReader("nm=" + pattern)
	res, err := c.httpClient.Post(searchURL, "application/x-www-form-urlencoded", searchBody)
	if err != nil {
		fmt.Printf("Error: %v", err)
		return nil, err
	}
	defer res.Body.Close()

	bodyData, err := io.ReadAll(res.Body)
	if err != nil {
		fmt.Printf("Error: %v", err)
		return nil, err
	}

	items, err := ParseSearchItems(&bodyData)
	if err != nil {
		fmt.Printf("Error: %v", err)
		return nil, err
	}

	return items, nil
}

func (c *RutrackerClient) DownloadTorrent(url string) ([]byte, error) {
	absoluteUrl := fmt.Sprintf("https://rutracker.org/forum/%v", url)
	res, err := c.httpClient.Get(absoluteUrl)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	bodyData, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	return bodyData, nil
}
