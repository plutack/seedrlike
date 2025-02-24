package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"os"

	_ "github.com/go-sql-driver/mysql"
)

func GetHealth(w http.ResponseWriter, r *http.Request) {
	// Check database connection
	dbStatus := "active"
	conn, err := sql.Open("mysql", os.Getenv("GOOSE_DBSTRING"))
	if err != nil {
		dbStatus = "inactive"
	} else {
		defer conn.Close()
		if err = conn.Ping(); err != nil {
			dbStatus = "inactive"
		}
	}

	// Determine HTTP status
	statusCode := http.StatusOK
	if dbStatus == "inactive" {
		statusCode = http.StatusInternalServerError
	}

	// Prepare JSON response
	response := map[string]string{
		"status":   "ok",
		"database": dbStatus,
	}

	// If database is inactive, update status
	if dbStatus == "inactive" {
		response["status"] = "error"
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(response)
}
