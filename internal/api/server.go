package api

import (
	"encoding/json"
	"net/http"

	"github.com/anacrolix/torrent"
	"github.com/plutack/seedrlike/internal/api/handlers"
	"github.com/plutack/seedrlike/internal/core/client"
	"github.com/plutack/seedrlike/internal/core/queue"

	"github.com/gorilla/mux"
)

const (
	GetMethod    = "GET"
	PostMethod   = "POST"
	DeleteMethod = "DELETE"
)

type Server struct {
	router *mux.Router
	client *torrent.Client
	queue  *queue.DownloadQueue
}

type Response struct {
	Message string `json:"message"`
	Status  bool   `json:"status"`
}

func (s *Server) registerRoutes() {
	// register routes to be used
	d := handlers.NewDownloadHandler(s.queue)

	s.router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {

		response := Response{
			Message: "Hello, World!",
			Status:  true,
		}

		// Set content type header
		w.Header().Set("Content-Type", "application/json")

		// Encode and send JSON response
		json.NewEncoder(w).Encode(response)
	}).Methods(GetMethod)
	s.router.HandleFunc("/downloads", d.CreateNewDownload).Methods(GetMethod, PostMethod)

	// router.HandleFunc("/downloads/{torrentID}", handlers.StopDownloadTaskHandler).Methods(DeleteMethod)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

var storagePath = "/home/plutack/Downloads/seedrlike"

func New() (*Server, error) {
	config := torrent.NewDefaultClientConfig()
	config.DataDir = storagePath
	c, err := client.New(config)
	if err != nil {
		return nil, err
	}

	q := queue.New()

	s := &Server{
		router: mux.NewRouter(),
		client: c,
		queue:  q,
	}
	s.registerRoutes()
	go queue.ProcessTasks(s.client, s.queue)
	return s, nil
}
