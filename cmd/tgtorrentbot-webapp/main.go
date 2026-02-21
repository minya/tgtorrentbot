package main

import (
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/minya/logger"
	"github.com/minya/rutracker"
	"github.com/odwrtw/transmission"
)

//go:embed static
var staticFiles embed.FS

// relativeDownloadRe matches the relative URL format returned by the rutracker library's Find().
var relativeDownloadRe = regexp.MustCompile(`^dl\.php\?t=\d+$`)

// validCategories is the single source of truth for valid download categories.
var validCategories = []string{"movies", "shows", "music", "musicvideos", "audiobooks", "others"}

type Config struct {
	BotToken             string
	TransmissionAddr     string
	TransmissionUser     string
	TransmissionPassword string
	RutrackerUsername    string
	RutrackerPassword    string
	DownloadPath         string
	LogLevel             string
	JellyfinURL          string
	JellyfinAPIKey       string
	IncompletePath       string
}

func loadConfig() Config {
	downloadPath := os.Getenv("TGT_DOWNLOADPATH")
	incompletePath := os.Getenv("TGT_INCOMPLETE_PATH")
	if incompletePath == "" && downloadPath != "" {
		incompletePath = filepath.Clean(filepath.Join(downloadPath, "..", "incomplete"))
	}

	return Config{
		BotToken:             strings.TrimSpace(os.Getenv("TGT_BOTTOKEN")),
		TransmissionAddr:     os.Getenv("TGT_RPC_ADDR"),
		TransmissionUser:     os.Getenv("TGT_RPC_USER"),
		TransmissionPassword: os.Getenv("TGT_RPC_PASSWORD"),
		RutrackerUsername:    os.Getenv("TGT_RUTRACKER_USERNAME"),
		RutrackerPassword:    os.Getenv("TGT_RUTRACKER_PASSWORD"),
		DownloadPath:         downloadPath,
		LogLevel:             os.Getenv("TGT_LOGLEVEL"),
		JellyfinURL:          os.Getenv("TGT_JELLYFIN_URL"),
		JellyfinAPIKey:       os.Getenv("TGT_JELLYFIN_API_KEY"),
		IncompletePath:       incompletePath,
	}
}

type App struct {
	config             Config
	transmissionClient *transmission.Client
	jellyfinClient     *jellyfinClient
}

type httpHandlerFunc func(http.ResponseWriter, *http.Request)
type appHandlerFunc func(int64, http.ResponseWriter, *http.Request)

func (app *App) makeHandler(allowedMethods []string, handler appHandlerFunc) httpHandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !slices.Contains(allowedMethods, r.Method) {
			w.Header().Set("Allow", strings.Join(allowedMethods, ", "))
			http.Error(w, `{"error": "method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		initDataObj, valid := validateRequest(r, w, app.config.BotToken)
		if !valid {
			return
		}
		w.Header().Set("Content-Type", "application/json")

		userID, err := initDataObj.userID()
		if err != nil {
			logger.Warn("Failed to extract user ID: %v", err)
			http.Error(w, `{"error": "invalid init data"}`, http.StatusBadRequest)
			return
		}

		// Call the actual handler function
		handler(userID, w, r)
	}
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

	config := loadConfig()


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
		jellyfinClient:     newJellyfinClient(config.JellyfinURL, config.JellyfinAPIKey),
	}

	http.HandleFunc("/api/torrents", app.makeHandler([]string{http.MethodGet}, app.handleTorrents))
	http.HandleFunc("/api/torrents/remove", app.makeHandler([]string{http.MethodPost, http.MethodDelete}, app.handleRemoveTorrent))
	http.HandleFunc("/api/torrents/download", app.makeHandler([]string{http.MethodPost}, app.handleDownloadTorrent))
	http.HandleFunc("/api/search", app.makeHandler([]string{http.MethodGet}, app.handleSearch))
	http.HandleFunc("/api/items", app.makeHandler([]string{http.MethodGet}, app.handleUnifiedItems))

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
	srv := &http.Server{
		Addr:              ":" + port,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		logger.Error(err, "Server failed")
		os.Exit(1)
	}
}

// userTorrents filters and converts Transmission torrents to TorrentInfo for the given user.
func userTorrents(torrents []*transmission.Torrent, userID int64) []TorrentInfo {
	userIDStr := fmt.Sprintf("%d", userID)
	var result []TorrentInfo
	for _, t := range torrents {
		if len(t.Labels) == 0 || t.Labels[0] != userIDStr {
			continue
		}
		category := "others"
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
	return result
}

func (app *App) handleTorrents(userID int64, w http.ResponseWriter, r *http.Request) {
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

	result := userTorrents(torrents, userID)

	if err := json.NewEncoder(w).Encode(result); err != nil {
		logger.Error(err, "Failed to encode torrents response")
	}
}

func (app *App) handleRemoveTorrent(userID int64, w http.ResponseWriter, r *http.Request) {
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

	userIDStr := fmt.Sprintf("%d", userID)
	var torrentToRemove *transmission.Torrent
	for _, t := range torrents {
		if t.ID == id {
			// Check that the requesting user owns this torrent.
			// Return 404 (not 403) to avoid leaking existence of other users' torrents.
			if len(t.Labels) == 0 || t.Labels[0] != userIDStr {
				http.Error(w, `{"error": "torrent not found"}`, http.StatusNotFound)
				return
			}
			torrentToRemove = t
			break
		}
	}

	if torrentToRemove == nil {
		http.Error(w, `{"error": "torrent not found"}`, http.StatusNotFound)
		return
	}

	err = app.transmissionClient.RemoveTorrents([]*transmission.Torrent{torrentToRemove}, true)
	if err != nil {
		logger.Error(err, "Failed to remove torrent")
		http.Error(w, `{"error": "failed to remove torrent"}`, http.StatusInternalServerError)
		return
	}

	logger.Info("Removed torrent %d: %s", id, torrentToRemove.Name)
	if err := json.NewEncoder(w).Encode(map[string]bool{"success": true}); err != nil {
		logger.Error(err, "Failed to encode remove response")
	}
}

func (app *App) handleDownloadTorrent(userID int64, w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB
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
		logger.Error(err, "Failed to set torrent labels, removing orphaned torrent %d", torrent.ID)
		if removeErr := app.transmissionClient.RemoveTorrents([]*transmission.Torrent{torrent}, false); removeErr != nil {
			logger.Error(removeErr, "Failed to remove orphaned torrent %d", torrent.ID)
		}
		http.Error(w, `{"error": "failed to set torrent labels"}`, http.StatusInternalServerError)
		return
	}

	logger.Info("Added torrent %d: %s [%s] for user %d", torrent.ID, torrent.Name, req.Category, userID)
	if err := json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"torrent": map[string]any{
			"id":   torrent.ID,
			"name": torrent.Name,
		},
	}); err != nil {
		logger.Error(err, "Failed to encode download response")
	}
}

func (app *App) handleSearch(userID int64, w http.ResponseWriter, r *http.Request) {
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

	if err := json.NewEncoder(w).Encode(result); err != nil {
		logger.Error(err, "Failed to encode search response")
	}
}

func (app *App) handleUnifiedItems(userID int64, w http.ResponseWriter, r *http.Request) {
	// 1. Get torrents for this user.
	torrents, err := app.transmissionClient.GetTorrents()
	if err != nil {
		logger.Error(err, "Failed to get torrents")
		http.Error(w, `{"error": "failed to get torrents"}`, http.StatusInternalServerError)
		return
	}

	ut := userTorrents(torrents, userID)

	// 2. Scan filesystem.
	scanner := &filesystemScanner{
		downloadPath:   app.config.DownloadPath,
		incompletePath: app.config.IncompletePath,
	}
	categories := validCategories
	fsItems := make(map[string][]FsItem)
	for _, cat := range categories {
		items, err := scanner.ScanCategory(cat)
		if err != nil {
			logger.Error(err, "Failed to scan filesystem category %s", cat)
			continue
		}
		if len(items) > 0 {
			fsItems[cat] = items
		}
	}

	incompleteItems, err := scanner.ScanIncomplete()
	if err != nil {
		logger.Error(err, "Failed to scan incomplete directory")
	}

	// 3. Get Jellyfin items.
	jellyfinItems, err := app.jellyfinClient.GetItems()
	if err != nil {
		logger.Error(err, "Failed to get Jellyfin items")
	}

	// 4. Merge and return.
	result := mergeItems(ut, fsItems, incompleteItems, jellyfinItems)
	if err := json.NewEncoder(w).Encode(result); err != nil {
		logger.Error(err, "Failed to encode unified items response")
	}
}

func validateRequest(r *http.Request, w http.ResponseWriter, botToken string) (*initData, bool) {
	initData := r.Header.Get("X-Telegram-Init-Data")
	if initData == "" {
		logger.Warn("Missing init data")
		http.Error(w, `{"error": "missing init data"}`, http.StatusBadRequest)
		return nil, false
	}

	initDataObj, err := newInitData(initData)
	if err != nil {
		logger.Warn("Invalid init data: %v", err)
		http.Error(w, `{"error": "invalid init data"}`, http.StatusUnauthorized)
		return nil, false
	}

	err = initDataObj.validate(botToken)
	if err != nil {
		logger.Warn("Invalid init data: %v", err)
		http.Error(w, `{"error": "invalid init data"}`, http.StatusUnauthorized)
		return nil, false
	}

	return initDataObj, true
}
