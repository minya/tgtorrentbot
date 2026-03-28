package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/minya/logger"
)

// wsClient represents a single WebSocket connection.
type wsClient struct {
	conn   *websocket.Conn
	userID int64
}

// wsHub manages active WebSocket clients grouped by userID.
type wsHub struct {
	mu      sync.Mutex
	clients map[int64]map[*wsClient]struct{}
}

func newWsHub() *wsHub {
	return &wsHub{
		clients: make(map[int64]map[*wsClient]struct{}),
	}
}

func (h *wsHub) register(c *wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.clients[c.userID] == nil {
		h.clients[c.userID] = make(map[*wsClient]struct{})
	}
	h.clients[c.userID][c] = struct{}{}
	logger.Info("WebSocket client registered: user_id=%d (total=%d)", c.userID, h.countLocked())
}

func (h *wsHub) unregister(c *wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if set, ok := h.clients[c.userID]; ok {
		delete(set, c)
		if len(set) == 0 {
			delete(h.clients, c.userID)
		}
	}
	logger.Info("WebSocket client unregistered: user_id=%d (total=%d)", c.userID, h.countLocked())
}

func (h *wsHub) hasClients() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.countLocked() > 0
}

// countLocked returns total client count. Must be called with mu held.
func (h *wsHub) countLocked() int {
	n := 0
	for _, set := range h.clients {
		n += len(set)
	}
	return n
}

// userIDs returns a snapshot of connected user IDs.
func (h *wsHub) userIDs() []int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	ids := make([]int64, 0, len(h.clients))
	for id := range h.clients {
		ids = append(ids, id)
	}
	return ids
}

// broadcast sends msg to all clients of the given userID.
func (h *wsHub) broadcast(userID int64, msg []byte) {
	h.mu.Lock()
	set := h.clients[userID]
	// Copy to avoid holding lock during writes.
	targets := make([]*wsClient, 0, len(set))
	for c := range set {
		targets = append(targets, c)
	}
	h.mu.Unlock()

	for _, c := range targets {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := c.conn.Write(ctx, websocket.MessageText, msg)
		cancel()
		if err != nil {
			logger.Debug("WebSocket write failed for user_id=%d: %v", userID, err)
			c.conn.CloseNow()
		}
	}
}

// handleWS upgrades an HTTP request to a WebSocket connection.
// Auth is passed via the Sec-WebSocket-Protocol header to avoid leaking
// initData in the URL (where it would appear in access logs and proxy logs).
// The client sends: new WebSocket(url, ["tg-auth." + base64url(initData)])
// The server strips the "tg-auth." prefix and decodes the initData.
func (app *App) handleWS(w http.ResponseWriter, r *http.Request) {
	// Extract initData from Sec-WebSocket-Protocol header.
	var initDataRaw string
	var authProto string
	for _, proto := range parseSubprotocols(r.Header.Get("Sec-WebSocket-Protocol")) {
		if after, ok := strings.CutPrefix(proto, "tg-auth."); ok {
			decoded, err := base64.RawURLEncoding.DecodeString(after)
			if err == nil {
				initDataRaw = string(decoded)
				authProto = proto
			}
			break
		}
	}
	if initDataRaw == "" {
		http.Error(w, `{"error": "missing auth"}`, http.StatusBadRequest)
		return
	}

	userID, err := app.authenticateInitData(initDataRaw)
	if err != nil {
		logger.Warn("WebSocket auth failed: %v", err)
		http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
		return
	}

	// Accept with the auth subprotocol so the client receives it back.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols: []string{authProto},
	})
	if err != nil {
		logger.Error(err, "WebSocket accept failed")
		return
	}

	// Use a connection-scoped context instead of r.Context(), which is
	// unreliable after websocket.Accept hijacks the connection.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := &wsClient{conn: conn, userID: userID}
	app.hub.register(client)
	defer func() {
		app.hub.unregister(client)
		conn.CloseNow()
	}()

	logger.Info("WebSocket connected: user_id=%d", userID)

	// Read loop: we don't expect messages from the client, but we need to
	// read to detect disconnection and handle control frames (ping/pong).
	for {
		_, _, err := conn.Read(ctx)
		if err != nil {
			logger.Debug("WebSocket read ended for user_id=%d: %v", userID, err)
			return
		}
	}
}

// startTorrentPoller runs a background loop that polls Transmission and
// broadcasts active torrent progress to connected WebSocket clients.
func (app *App) startTorrentPoller(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	// prevPercent tracks the last known PercentDone per torrent ID to detect completions.
	prevPercent := make(map[int]float64)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !app.hub.hasClients() {
				continue
			}
			app.pollAndBroadcast(prevPercent)
		}
	}
}

func (app *App) pollAndBroadcast(prevPercent map[int]float64) {
	torrents, err := app.transmissionClient.GetTorrents()
	if err != nil {
		logger.Error(err, "WebSocket poller: failed to get torrents")
		return
	}

	for _, userID := range app.hub.userIDs() {
		ut := userTorrents(torrents, userID)

		var updates []TorrentUpdate
		for _, t := range ut {
			prev, known := prevPercent[t.ID]
			isComplete := known && prev < 100 && t.PercentDone >= 100

			if t.PercentDone < 100 || isComplete {
				updates = append(updates, TorrentUpdate{
					ID:               t.ID,
					Name:             t.Name,
					PercentDone:      t.PercentDone,
					Category:         t.Category,
					RateDownload:     t.RateDownload,
					Eta:              t.Eta,
					PeersConnected:   t.PeersConnected,
					PeersSendingToUs: t.PeersSendingToUs,
					TotalSize:        t.TotalSize,
					IsComplete:       isComplete,
				})
			}

			prevPercent[t.ID] = t.PercentDone
		}

		var msg WSMessage
		if len(updates) > 0 {
			msg = WSMessage{Type: "torrentUpdate", Payload: updates}
		} else {
			msg = WSMessage{Type: "noActive", Payload: nil}
		}

		data, err := json.Marshal(msg)
		if err != nil {
			logger.Error(err, "WebSocket poller: failed to marshal message")
			continue
		}

		app.hub.broadcast(userID, data)
	}

	// Clean up prevPercent for torrents that no longer exist.
	activeIDs := make(map[int]struct{})
	for _, t := range torrents {
		activeIDs[t.ID] = struct{}{}
	}
	for id := range prevPercent {
		if _, ok := activeIDs[id]; !ok {
			delete(prevPercent, id)
		}
	}
}

// parseSubprotocols parses the Sec-WebSocket-Protocol header value
// into individual protocol names.
func parseSubprotocols(header string) []string {
	if header == "" {
		return nil
	}
	var protos []string
	for _, p := range strings.Split(header, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			protos = append(protos, p)
		}
	}
	return protos
}
