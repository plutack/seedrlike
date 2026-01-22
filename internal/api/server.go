package api

import (
	"database/sql"
	"net/http"
	"os"

	"github.com/anacrolix/torrent"
	_ "github.com/go-sql-driver/mysql"
	"github.com/gorilla/mux"
	"github.com/plutack/go-gofile/api"
	"github.com/plutack/seedrlike/internal/api/handlers"
	"github.com/plutack/seedrlike/internal/core/client"
	"github.com/plutack/seedrlike/internal/core/queue"
	ws "github.com/plutack/seedrlike/internal/core/websocket"
	database "github.com/plutack/seedrlike/internal/database/sqlc"
	"github.com/plutack/seedrlike/views/assets"
)

const (
	GetMethod    = "GET"
	PostMethod   = "POST"
	DeleteMethod = "DELETE"
)

type Server struct {
	router           *mux.Router
	torrentClient    *torrent.Client
	queue            *queue.DownloadQueue
	gofileClient     *api.Api
	rootFolderID     string
	db               *sql.DB
	dbQueries        *database.Queries
	websocketManager *ws.WebsocketManager
}

type Response struct {
	Message string `json:"message"`
	Status  bool   `json:"status"`
}

var storagePath = "/home/plutack/Downloads/seedrlike"

func (s *Server) registerRoutes() {
	// Static files first - NO Middleware
	s.router.PathPrefix("/assets/").Handler(http.StripPrefix("/assets/", http.FileServer(http.FS(assets.Assets))))

	// Subrouter for application routes - WITH Middleware
	// We use PathPrefix("/") but rely on order: assets are matched first.
	apiRouter := s.router.PathPrefix("/").Subrouter()
	apiRouter.Use(handlers.AuthMiddleware)

	d := handlers.NewDownloadHandler(s.queue)
	authHandler := handlers.NewAuthHandler(s.dbQueries)

	apiRouter.HandleFunc("/login", authHandler.LoginPage).Methods(GetMethod)
	apiRouter.HandleFunc("/login", authHandler.Login).Methods(PostMethod)
	apiRouter.HandleFunc("/register", authHandler.RegisterPage).Methods(GetMethod)
	apiRouter.HandleFunc("/register", authHandler.Register).Methods(PostMethod)
	apiRouter.HandleFunc("/logout", authHandler.Logout).Methods(PostMethod)

	apiRouter.HandleFunc("/", handlers.GetTorrentsFromDBHomepage(s.dbQueries, s.rootFolderID)).Methods(GetMethod)
	apiRouter.HandleFunc("/downloads", d.CreateNewDownload).Methods(PostMethod)
	apiRouter.HandleFunc("/downloads/{ID}", d.StopDownload).Methods(DeleteMethod)
	apiRouter.HandleFunc("/contents", handlers.DeleteStaleContentFromDB(s.dbQueries, s.gofileClient, s.db)).Methods(DeleteMethod)
	apiRouter.HandleFunc("/contents/{ID}", handlers.GetTorrentsFromDB(s.dbQueries, s.rootFolderID)).Methods(GetMethod)
	apiRouter.HandleFunc("/contents/{ID}", handlers.DeleteContentFromDB(s.dbQueries, s.gofileClient, s.db)).Methods(DeleteMethod)
	apiRouter.HandleFunc("/ws", handlers.UpgradeRequest(s.websocketManager))
	apiRouter.HandleFunc("/health", handlers.GetHealth).Methods(GetMethod)
}

func (s *Server) serveStatic() {
	// Moved to registerRoutes to control order and middleware application
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

	timeout := 0
	retryCount := 2
	u := api.New(&api.Options{
		RetryCount: &retryCount,
		Timeout:    &timeout,
	})
	q := queue.New()
	wm := ws.New()

	userInfo, err := u.GetAccountID()
	if err != nil {
		return nil, err
	}
	accountInfo, err := u.GetAccountInformation(userInfo.Data.ID)
	if err != nil {
		return nil, err
	}
	conn, err := sql.Open("mysql", os.Getenv("GOOSE_DBSTRING")+"?parseTime=true")
	if err != nil {
		panic(err)
	}
	db := database.New(conn)
	r := accountInfo.Data.RootFolder
	// make rootFolder public
	u.UpdateContent(r, "public", true)
	s := &Server{
		router:           mux.NewRouter(),
		torrentClient:    c,
		queue:            q,
		gofileClient:     u,
		rootFolderID:     r,
		db:               conn,
		dbQueries:        db,
		websocketManager: wm,
	}
	s.registerRoutes()
	go wm.Run()
	go queue.ProcessTasks(s.torrentClient, s.queue, s.gofileClient, s.rootFolderID, s.dbQueries, wm)
	return s, nil
}
