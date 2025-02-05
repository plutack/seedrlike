package handlers

import (
	"log"
	"net/http"

	"github.com/gorilla/websocket"
	ws "github.com/plutack/seedrlike/internal/core/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func UpgradeRequest(wm *ws.WebsocketManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Upgrade HTTP request to WebSocket connection
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println("Error upgrading request:", err)
			http.Error(w, "Could not open WebSocket connection", http.StatusInternalServerError)
			return
		}

		log.Println("A new user connected")
		wm.RegisterClient(conn)
		defer wm.UnregisterClient(conn)

		// Keep connection open to handle potential disconnections
		select {}
	}
}
