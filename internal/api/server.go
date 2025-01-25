package api

import (
	"context"
	"net/http"

	"github.com/anacrolix/torrent"
	"github.com/plutack/go-gofile/api"
	"github.com/plutack/seedrlike/internal/api/handlers"
	"github.com/plutack/seedrlike/internal/core/client"
	"github.com/plutack/seedrlike/internal/core/queue"
	"github.com/plutack/seedrlike/views/home"

	"github.com/gorilla/mux"
)

const (
	GetMethod    = "GET"
	PostMethod   = "POST"
	DeleteMethod = "DELETE"
)

type Server struct {
	router        *mux.Router
	torrentClient *torrent.Client
	queue         *queue.DownloadQueue
	gofileClient  *api.Api
	rootFolderID  string
}

type Response struct {
	Message string `json:"message"`
	Status  bool   `json:"status"`
}

var storagePath = "/home/plutack/Downloads/seedrlike"

func (s *Server) registerRoutes() {
	// register routes to be used
	d := handlers.NewDownloadHandler(s.queue)

	s.router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {

		// Set content type header
		w.Header().Set("Content-Type", "text/html")
		homeComponent := home.Home()
		homeComponent.Render(context.Background(), w)
	}).Methods(GetMethod)
	s.router.HandleFunc("/downloads", d.CreateNewDownload).Methods(GetMethod, PostMethod)

	// router.HandleFunc("/downloads/{torrentID}", handlers.StopDownloadTaskHandler).Methods(DeleteMethod)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func New() (*Server, error) {
	config := torrent.NewDefaultClientConfig()
	config.DataDir = storagePath
	c, err := client.New(config)
	if err != nil {
		return nil, err
	}

	u := api.New(nil)
	q := queue.New()

	userInfo, err := u.GetAccountID()
	if err != nil {
		return nil, err
	}
	accountInfo, err := u.GetAccountInformation(userInfo.Data.ID)
	if err != nil {
		return nil, err
	}
	r := accountInfo.Data.RootFolder
	s := &Server{
		router:        mux.NewRouter(),
		torrentClient: c,
		queue:         q,
		gofileClient:  u,
		rootFolderID:  r,
	}
	s.registerRoutes()
	go queue.ProcessTasks(s.torrentClient, s.queue, s.gofileClient, s.rootFolderID)
	return s, nil
}
