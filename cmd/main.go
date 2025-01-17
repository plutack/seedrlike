package main

import (
	"log"
	"net/http"

	"github.com/plutack/seedrlike/internal/api"
)

func main() {
	server := api.New()

	log.Fatal(http.ListenAndServe(":3000", server))
}
