package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/minya/logger"
	"github.com/minya/rutracker"
	"github.com/odwrtw/transmission"
)

//go:embed static
var staticFiles embed.FS

// relativeDownloadRe matches the relative URL format returned by the rutracker library's Find().
var relativeDownloadRe = regexp.MustCompile(`^dl\.php\?t=\d+$`)

type Config struct {
	BotToken             string
	TransmissionAddr     string
	TransmissionUser     string
	TransmissionPassword string
	RutrackerUsername    string
	RutrackerPassword    string
	DownloadPath         string
	LogLevel             string
}

type App struct {
	config             Config
	transmissionClient *transmission.Client
}

func main() {
	logLevel := "info"
	if val, exists := os.LookupEnv("TGT_LOGLEVEL"); exists {
		logLevel = val
	}
	logger.InitLogger(logger.Config{
		Level:  logLevel,
		Pretty: true,
		Output: os.Stdout,
	})

	config := Config{
		BotToken:             strings.TrimSpace(os.Getenv("TGT_BOTTOKEN")),
		TransmissionAddr:     os.Getenv("TGT_RPC_ADDR"),
		TransmissionUser:     os.Getenv("TGT_RPC_USER"),
		TransmissionPassword: os.Getenv("TGT_RPC_PASSWORD"),
		RutrackerUsername:    os.Getenv("TGT_RUTRACKER_USERNAME"),
		RutrackerPassword:    os.Getenv("TGT_RUTRACKER_PASSWORD"),
		DownloadPath:         os.Getenv("TGT_DOWNLOADPATH"),
		LogLevel:             os.Getenv("TGT_LOGLEVEL"),
	}

	if config.BotToken == "" {
		logger.Error(nil, "TGT_BOTTOKEN environment variable is not set")
		os.Exit(1)
	}
	if config.DownloadPath == "" {
		logger.Error(nil, "TGT_DOWNLOADPATH environment variable is not set")
		os.Exit(1)
	}

	transmissionClient, err := transmission.New(transmission.Config{
		Address:  config.TransmissionAddr,
		User:     config.TransmissionUser,
		Password: config.TransmissionPassword,
	})
	if err != nil {
		logger.Error(err, "Failed to create transmission client")
		os.Exit(1)
	}

	app := &App{
		config:             config,
		transmissionClient: transmissionClient,
	}

	http.HandleFunc("/api/torrents", app.handleTorrents)
	http.HandleFunc("/api/torrents/remove", app.handleRemoveTorrent)
	http.HandleFunc("/api/torrents/download", app.handleDownloadTorrent)
	http.HandleFunc("/api/search", app.handleSearch)

	subFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		logger.Error(err, "Failed to create sub filesystem")
		os.Exit(1)
	}
	http.Handle("/", http.FileServer(http.FS(subFS)))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	logger.Info("Starting server on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		logger.Error(err, "Server failed")
		os.Exit(1)
	}
}

type TorrentInfo struct {
	ID          int     `json:"id"`
	Name        string  `json:"name"`
	PercentDone float64 `json:"percentDone"`
	Category    string  `json:"category"`
	TotalSize   int64   `json:"totalSize"`
	AddedDate   int     `json:"addedDate"`
}

func (app *App) handleTorrents(w http.ResponseWriter, r *http.Request) {
	if !validateRequest(r, w, app.config.BotToken) {
		return
	}
	w.Header().Set("Content-Type", "application/json")

	torrents, err := app.transmissionClient.GetTorrents()
	if err != nil {
		logger.Error(err, "Failed to get torrents")
		http.Error(w, `{"error": "failed to get torrents"}`, http.StatusInternalServerError)
		return
	}

	// Sort by ID descending (most recent first)
	sort.Slice(torrents, func(i, j int) bool {
		return torrents[i].ID > torrents[j].ID
	})

	result := make([]TorrentInfo, 0, len(torrents))
	for _, t := range torrents {
		category := "Unknown"
		if len(t.Labels) >= 2 {
			category = t.Labels[1]
		}
		result = append(result, TorrentInfo{
			ID:          t.ID,
			Name:        t.Name,
			PercentDone: t.PercentDone * 100,
			Category:    category,
			TotalSize:   t.TotalSize,
			AddedDate:   t.AddedDate,
		})
	}

	json.NewEncoder(w).Encode(result)
}

func (app *App) handleRemoveTorrent(w http.ResponseWriter, r *http.Request) {
	if !validateRequest(r, w, app.config.BotToken) {
		return
	}
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, `{"error": "method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		http.Error(w, `{"error": "id parameter is required"}`, http.StatusBadRequest)
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, `{"error": "invalid id"}`, http.StatusBadRequest)
		return
	}

	torrents, err := app.transmissionClient.GetTorrents()
	if err != nil {
		logger.Error(err, "Failed to get torrents")
		http.Error(w, `{"error": "failed to get torrents"}`, http.StatusInternalServerError)
		return
	}

	var torrentToRemove *transmission.Torrent
	for _, t := range torrents {
		if t.ID == id {
			torrentToRemove = t
			break
		}
	}

	if torrentToRemove == nil {
		http.Error(w, `{"error": "torrent not found"}`, http.StatusNotFound)
		return
	}

	err = app.transmissionClient.RemoveTorrents([]*transmission.Torrent{torrentToRemove}, false)
	if err != nil {
		logger.Error(err, "Failed to remove torrent")
		http.Error(w, `{"error": "failed to remove torrent"}`, http.StatusInternalServerError)
		return
	}

	logger.Info("Removed torrent %d: %s", id, torrentToRemove.Name)
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

type DownloadRequest struct {
	DownloadURL string `json:"downloadUrl"`
	Category    string `json:"category"`
}

func (app *App) handleDownloadTorrent(w http.ResponseWriter, r *http.Request) {
	if !validateRequest(r, w, app.config.BotToken) {
		return
	}
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		http.Error(w, `{"error": "method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	initData := r.Header.Get("X-Telegram-Init-Data")
	userID, err := extractUserID(initData)
	if err != nil {
		logger.Warn("Failed to extract user ID: %v", err)
		http.Error(w, `{"error": "invalid init data"}`, http.StatusBadRequest)
		return
	}

	var req DownloadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.Warn("Failed to decode request body: %v", err)
		http.Error(w, `{"error": "invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.DownloadURL == "" {
		logger.Warn("Empty downloadUrl in request")
		http.Error(w, `{"error": "downloadUrl is required"}`, http.StatusBadRequest)
		return
	}

	parsedURL, err := url.Parse(req.DownloadURL)
	isAbsoluteRutracker := err == nil && (parsedURL.Host == "rutracker.org" || parsedURL.Host == "www.rutracker.org")
	isRelativeDownload := err == nil && parsedURL.Host == "" && relativeDownloadRe.MatchString(req.DownloadURL)
	if !isAbsoluteRutracker && !isRelativeDownload {
		logger.Warn("Invalid downloadUrl: %s", req.DownloadURL)
		http.Error(w, `{"error": "invalid downloadUrl: must be a rutracker.org URL"}`, http.StatusBadRequest)
		return
	}

	logger.Info("Download request from user %d: %s [%s]", userID, req.DownloadURL, req.Category)

	if req.Category == "" {
		http.Error(w, `{"error": "category is required"}`, http.StatusBadRequest)
		return
	}

	validCategories := []string{"movies", "shows", "music", "musicvideos", "audiobooks", "others"}
	if !slices.Contains(validCategories, req.Category) {
		http.Error(w, `{"error": "invalid category"}`, http.StatusBadRequest)
		return
	}

	client, err := rutracker.NewAuthenticatedRutrackerClient(
		app.config.RutrackerUsername,
		app.config.RutrackerPassword,
	)
	if err != nil {
		logger.Error(err, "Failed to authenticate with rutracker")
		http.Error(w, `{"error": "failed to authenticate with rutracker"}`, http.StatusInternalServerError)
		return
	}

	torrentData, err := client.DownloadTorrent(req.DownloadURL)
	if err != nil {
		logger.Error(err, "Failed to download torrent")
		http.Error(w, `{"error": "failed to download torrent"}`, http.StatusInternalServerError)
		return
	}

	torrentBase64 := base64.StdEncoding.EncodeToString(torrentData)
	downloadPath := fmt.Sprintf("%s/%s/", app.config.DownloadPath, req.Category)
	torrent, err := app.transmissionClient.AddTorrent(transmission.AddTorrentArg{
		Metainfo:    torrentBase64,
		DownloadDir: downloadPath,
	})
	if err != nil {
		logger.Error(err, "Failed to add torrent to Transmission")
		http.Error(w, `{"error": "failed to add torrent"}`, http.StatusInternalServerError)
		return
	}

	labels := []string{fmt.Sprintf("%d", userID), req.Category}
	err = torrent.Set(transmission.SetTorrentArg{
		Labels: labels,
	})
	if err != nil {
		logger.Error(err, "Failed to set torrent labels")
		// Don't fail the request, torrent is already added
	}

	logger.Info("Added torrent %d: %s [%s] for user %d", torrent.ID, torrent.Name, req.Category, userID)
	json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"torrent": map[string]any{
			"id":   torrent.ID,
			"name": torrent.Name,
		},
	})
}

type SearchResult struct {
	Title       string `json:"title"`
	Size        string `json:"size"`
	Seeders     int    `json:"seeders"`
	DownloadURL string `json:"downloadUrl"`
}

func (app *App) handleSearch(w http.ResponseWriter, r *http.Request) {
	if !validateRequest(r, w, app.config.BotToken) {
		return
	}
	w.Header().Set("Content-Type", "application/json")

	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, `{"error": "query parameter 'q' is required"}`, http.StatusBadRequest)
		return
	}

	client, err := rutracker.NewAuthenticatedRutrackerClient(
		app.config.RutrackerUsername,
		app.config.RutrackerPassword,
	)
	if err != nil {
		logger.Error(err, "Failed to authenticate with rutracker")
		http.Error(w, `{"error": "failed to authenticate with rutracker"}`, http.StatusInternalServerError)
		return
	}

	items, err := client.Find(query)
	if err != nil {
		logger.Error(err, "Failed to search rutracker")
		http.Error(w, `{"error": "search failed"}`, http.StatusInternalServerError)
		return
	}

	// Sort by seeders descending
	sort.Slice(items, func(i, j int) bool {
		return items[i].Seeders > items[j].Seeders
	})

	// Limit to 20 results
	if len(items) > 20 {
		items = items[:20]
	}

	result := make([]SearchResult, 0, len(items))
	for _, item := range items {
		result = append(result, SearchResult{
			Title:       item.Title,
			Size:        fmt.Sprintf("%v%s", item.Size.Size, item.Size.Unit),
			Seeders:     item.Seeders,
			DownloadURL: item.DownloadURL,
		})
	}

	json.NewEncoder(w).Encode(result)
}

func extractUserID(initData string) (int64, error) {
	q, err := url.ParseQuery(initData)
	if err != nil {
		return 0, fmt.Errorf("failed to parse init data: %w", err)
	}

	userJSON := q.Get("user")
	if userJSON == "" {
		return 0, fmt.Errorf("user not found in init data")
	}

	var user struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(userJSON), &user); err != nil {
		return 0, fmt.Errorf("failed to parse user data: %w", err)
	}

	return user.ID, nil
}

func validateRequest(r *http.Request, w http.ResponseWriter, botToken string) bool {
	initData := r.Header.Get("X-Telegram-Init-Data")
	if initData == "" {
		logger.Warn("Missing init data")
		http.Error(w, `{"error": "missing init data"}`, http.StatusBadRequest)
		return false
	}

	err := validateInitData(initData, botToken)
	if err != nil {
		logger.Warn("Invalid init data: %v", err)
		http.Error(w, `{"error": "invalid init data"}`, http.StatusUnauthorized)
		return false
	}

	return true
}

func validateInitData(initData string, botToken string) error {
	q, err := url.ParseQuery(initData)
	if err != nil {
		return fmt.Errorf("failed to parse init data: %w", err)
	}

	hash := q.Get("hash")
	if hash == "" {
		return fmt.Errorf("missing hash in init data")
	}

	var keys []string
	for key := range q {
		if key != "hash" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	var pairs []string
	for _, key := range keys {
		value := q.Get(key)
		pairs = append(pairs, fmt.Sprintf("%s=%s", key, value))
	}

	dataCheckString := strings.Join(pairs, "\n")

	secretKey := computeHMACSHA256Bytes([]byte(botToken), []byte("WebAppData"))

	expectedHash := computeHMACSHA256Hex([]byte(dataCheckString), secretKey)

	if !hmac.Equal([]byte(hash), []byte(expectedHash)) {
		return fmt.Errorf("invalid init data: hash mismatch")
	}
	return nil
}

func computeHMACSHA256Bytes(data, key []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func computeHMACSHA256Hex(data, key []byte) string {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return fmt.Sprintf("%x", h.Sum(nil))
}
