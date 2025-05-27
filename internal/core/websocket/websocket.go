package ws

import (
	"fmt"
	"log"
	"sync"

	"github.com/gorilla/websocket"
)

type TorrentUpdate struct {
	Type     string  `json:"type"`
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Status   string  `json:"status"`
	Progress float64 `json:"progress"`
	Speed    string  `json:"speed"`
	ETA      string  `json:"eta"`
}

type RefreshUpdate struct {
	Type    string `json:"type"`
	Message string `json:"message "`
}

func (u TorrentUpdate) Stringify() string {
	return fmt.Sprintf("ID: %s\nName: %s\nStatus: %s\n", u.ID, u.Name, u.Status)
}

func (u RefreshUpdate) Stringify() string {
	return fmt.Sprintf("Type: %s\nMessage: %s\n", u.Type, u.Message)
}

type Update interface {
	Stringify() string
}

type WebsocketManager struct {
	clients         map[*websocket.Conn]bool
	broadcast       chan Update
	register        chan *websocket.Conn
	unregister      chan *websocket.Conn
	progressData    map[string]float64
	activeDownloads map[string]TorrentUpdate // add other info here?
	mu              sync.Mutex
}

func New() *WebsocketManager {
	return &WebsocketManager{
		clients:         make(map[*websocket.Conn]bool),
		broadcast:       make(chan (Update)),
		register:        make(chan (*websocket.Conn)),
		unregister:      make(chan (*websocket.Conn)),
		progressData:    make(map[string]float64),
		activeDownloads: make(map[string]TorrentUpdate),
	}

}

func (wm *WebsocketManager) Run() {
	for {
		select {
		case c := <-wm.register:
			wm.clients[c] = true
			for _, update := range wm.activeDownloads {
				err := c.WriteJSON(update)
				if err != nil {
					log.Println("Error sending active downloads:", err)
					c.Close()
					delete(wm.clients, c)
				}
			}

		case c := <-wm.unregister:
			if _, ok := wm.clients[c]; ok {
				delete(wm.clients, c)
				c.Close()
			}

		case message := <-wm.broadcast:
			for c := range wm.clients {
				err := c.WriteJSON(message)
				if err != nil {
					log.Println("WebSocket error:", err)
					c.Close()
					delete(wm.clients, c)
				}
			}
		}
	}
}

func (wm *WebsocketManager) SendProgress(u Update) {
	switch v := u.(type) {
	case TorrentUpdate:
		if v.Status == "downloading" || v.Status == "pending" || v.Status == "uploading" || v.Status == "zipping" {
			wm.activeDownloads[v.ID] = v
		}
		if v.Status == "completed" || v.Status == "failed" || v.Status == "stopped" {
			delete(wm.activeDownloads, v.ID)
		}
	case RefreshUpdate:
	}

	wm.broadcast <- u
}

func (wm *WebsocketManager) RegisterClient(c *websocket.Conn) {
	wm.register <- c
}

func (wm *WebsocketManager) UnregisterClient(c *websocket.Conn) {
	wm.unregister <- c
}
