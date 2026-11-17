package api

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/plutack/seedrlike/internal/database/migrations"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/plutack/seedrlike/internal/jobs"
	"github.com/plutack/seedrlike/internal/notifications"
	"github.com/plutack/seedrlike/internal/observability"

	"github.com/anacrolix/torrent"
	"github.com/go-sql-driver/mysql"
	"github.com/gorilla/mux"
	"github.com/plutack/go-gofile/api"
	"github.com/plutack/seedrlike/internal/api/handlers"
	"github.com/plutack/seedrlike/internal/core/client"
	"github.com/plutack/seedrlike/internal/core/queue"
	"github.com/plutack/seedrlike/internal/core/search"
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
	previewClient    *torrent.Client
	jobStore         *jobs.Store
	dataDir          string
}

type Response struct {
	Message string `json:"message"`
	Status  bool   `json:"status"`
}

func storageDirectory() (string, error) {
	p := os.Getenv("DOWNLOAD_DIR")
	if p == "" {
		p = "./data/downloads"
	}
	return filepath.Abs(p)
}

func (s *Server) registerRoutes() {
	s.router.Use(observability.HTTP)
	// Static files first - NO Middleware
	s.router.PathPrefix("/assets/").Handler(http.StripPrefix("/assets/", http.FileServer(http.FS(assets.Assets))))

	// Subrouter for application routes - WITH Middleware
	// We use PathPrefix("/") but rely on order: assets are matched first.
	apiRouter := s.router.PathPrefix("/").Subrouter()
	apiRouter.Use(handlers.AuthMiddleware)
	apiRouter.Use(handlers.SameOriginMiddleware)
	apiRouter.Use(handlers.AdminContextMiddleware(s.dbQueries))

	d := handlers.NewDownloadHandler(s.queue)
	d.SetPreviewClient(s.previewClient)
	settings := handlers.NewSettingsHandler(s.db)
	admin := handlers.NewAdminHandler(s.dbQueries, s.queue, s.jobStore, s.dataDir)
	apiRouter.HandleFunc("/settings", settings.Settings).Methods(GetMethod)
	apiRouter.HandleFunc("/settings", settings.Save).Methods(PostMethod)
	apiRouter.HandleFunc("/settings/verify", settings.Verify).Methods(GetMethod, PostMethod)
	apiRouter.HandleFunc("/settings/test", settings.Test).Methods(PostMethod)
	apiRouter.HandleFunc("/admin", admin.Dashboard).Methods(GetMethod)
	apiRouter.HandleFunc("/downloads/preview", d.Preview).Methods(PostMethod)
	apiRouter.HandleFunc("/downloads/torrent", d.UploadTorrent).Methods(PostMethod)
	authHandler := handlers.NewAuthHandler(s.dbQueries)
	// Search talks to the external torrentgo service (TORRENTGO_URL, default
	// http://localhost:8080). Credentials must match the service's USERNAME/PASSWORD.
	searchHandler := handlers.NewSearchHandler(search.New(
		os.Getenv("TORRENTGO_URL"),
		os.Getenv("TORRENTGO_USERNAME"),
		os.Getenv("TORRENTGO_PASSWORD"),
	))

	apiRouter.HandleFunc("/login", authHandler.LoginPage).Methods(GetMethod)
	apiRouter.HandleFunc("/login", authHandler.Login).Methods(PostMethod)
	apiRouter.HandleFunc("/register", authHandler.RegisterPage).Methods(GetMethod)
	apiRouter.HandleFunc("/register", authHandler.Register).Methods(PostMethod)
	apiRouter.HandleFunc("/logout", authHandler.Logout).Methods(PostMethod)

	apiRouter.HandleFunc("/", handlers.GetTorrentsFromDBHomepage(s.dbQueries, s.rootFolderID)).Methods(GetMethod)
	apiRouter.HandleFunc("/search", searchHandler.Search).Methods(GetMethod)
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
	// Schema must be ready before torrent clients, remote calls, or workers start.
	cfg, err := mysql.ParseDSN(os.Getenv("GOOSE_DBSTRING"))
	if err != nil {
		slog.Error("startup_step_failed", "step", "database_configuration", "error_type", fmt.Sprintf("%T", err))
		return nil, fmt.Errorf("database configuration: %w", err)
	}
	if cfg.DBName == "" {
		return nil, fmt.Errorf("GOOSE_DBSTRING must select an existing database")
	}
	cfg.ParseTime = true
	conn, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, err
	}
	ready := false
	defer func() {
		if !ready {
			conn.Close()
		}
	}()
	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancelStartup()
	if err = conn.PingContext(startupCtx); err != nil {
		slog.Error("startup_step_failed", "step", "database_connect", "error_type", fmt.Sprintf("%T", err))
		return nil, fmt.Errorf("database: %w", err)
	}
	slog.Info("startup_step_completed", "step", "database_connect")
	if err = migrations.Up(startupCtx, conn); err != nil {
		return nil, err
	}
	dataDir, err := storageDirectory()
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(dataDir, 0700); err != nil {
		slog.Error("startup_step_failed", "step", "download_directory", "error_type", fmt.Sprintf("%T", err))
		return nil, err
	}
	config := torrent.NewDefaultClientConfig()
	config.DataDir = dataDir
	c, err := client.New(config)
	if err != nil {
		return nil, err
	}

	timeout := 0
	retryCount := 2
	apiToken := os.Getenv("GOFILE_API_KEY")
	u := api.New(&api.Options{
		APIToken:   &apiToken,
		RetryCount: &retryCount,
		Timeout:    &timeout,
	})
	q := queue.New()
	wm := ws.New()

	userInfo, err := u.GetAccountID()
	if err != nil {
		slog.Error("startup_step_failed", "step", "gofile_account", "error_type", fmt.Sprintf("%T", err))
		c.Close()
		return nil, err
	}
	accountInfo, err := u.GetAccountInformation(userInfo.Data.ID)
	if err != nil {
		slog.Error("startup_step_failed", "step", "gofile_account_information", "error_type", fmt.Sprintf("%T", err))
		c.Close()
		return nil, err
	}
	db := database.New(conn)
	store := jobs.New(conn)
	if err = q.Configure(store, dataDir, notifications.New(conn).Notify); err != nil {
		c.Close()
		conn.Close()
		slog.Error("startup_step_failed", "step", "queue_recovery", "error_type", fmt.Sprintf("%T", err))
		return nil, fmt.Errorf("recover jobs after schema migration: %w", err)
	}
	previewConfig := torrent.NewDefaultClientConfig()
	previewConfig.DataDir = filepath.Join(dataDir, "metadata")
	previewConfig.ListenPort = 0
	previewConfig.NoUpload = true
	previewClient, err := client.New(previewConfig)
	if err != nil {
		c.Close()
		conn.Close()
		return nil, err
	}
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
		previewClient:    previewClient,
		jobStore:         store,
		dataDir:          dataDir,
	}
	s.registerRoutes()
	ready = true
	go wm.Run()
	go queue.ProcessTasks(s.torrentClient, s.queue, s.gofileClient, s.rootFolderID, s.dbQueries, wm)
	return s, nil
}
