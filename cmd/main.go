package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/plutack/seedrlike/internal/api"
)

func main() {
	server := api.New()

	fmt.Println("server starting on port 3000...")
	log.Fatal(http.ListenAndServe(":3000", server))
}
