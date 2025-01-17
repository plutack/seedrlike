package api

import (
	"github.com/plutack/seedrlike/internal/api/handlers"

	"github.com/gorilla/mux"
)

const (
	GetMethod    = "GET"
	PostMethod   = "POST"
	DeleteMethod = "DELETE"
)

func New() *mux.Router {
	router := mux.NewRouter()

	// register routes to be used
	router.HandleFunc("/downloads", handlers.CreateNewDownloadHandler).Methods(GetMethod, PostMethod)
	// router.HandleFunc("/downloads/{torrentID}", handlers.StopDownloadTaskHandler).Methods(DeleteMethod)
	return router
}
