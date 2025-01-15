package api

import (
	"github.com/plutack/seedrlike/internal/api/handlers"

	"github.com/gorilla/mux"
)

const (
	GetMethod  = "GET"
	PostMethod = "POST"
)

func New() *mux.Router {
	router := mux.NewRouter()

	// register routes to be used
	router.HandleFunc("/create", handlers.CreateNewDownloadHandler).Methods(PostMethod)
	router.HandleFunc("/", handlers.GetDownloadsHandler).Methods(GetMethod)
	router.HandleFunc("/terminate", stopDownloadTaskHandler)
	return router
}
