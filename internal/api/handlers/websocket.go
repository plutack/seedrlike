package handlers

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gorilla/websocket"
	ws "github.com/plutack/seedrlike/internal/core/websocket"
)

// Gorilla's default origin check permits only same-host browser connections.
var upgrader = websocket.Upgrader{}

func UpgradeRequest(wm *ws.WebsocketManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Upgrade HTTP request to WebSocket connection
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Error("WebSocket upgrade failed", "operation", "websocket_upgrade", "reason", "upgrade_failed", "error_type", fmt.Sprintf("%T", err))
			http.Error(w, "Could not open WebSocket connection", http.StatusInternalServerError)
			return
		}

		slog.Debug("WebSocket connected", "operation", "websocket_connect")
		userID, _ := r.Context().Value(UserIDKey).(string)
		var userIDPtr *string
		if userID != "" {
			userIDPtr = &userID
		}
		wm.RegisterClient(conn, userIDPtr)
		defer wm.UnregisterClient(conn)

		// Read control frames so closed sockets release their handler and registration.
		conn.SetReadLimit(1024)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}
}
