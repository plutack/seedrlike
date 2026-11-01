package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/plutack/seedrlike/internal/api"
	"github.com/plutack/seedrlike/internal/observability"
)

func main() {
	if err := observability.Configure(os.Stdout, os.Getenv("LOG_LEVEL")); err != nil {
		slog.Error("logging_configuration_failed", "reason", "invalid_log_level")
		os.Exit(1)
	}
	slog.Info("application_starting")
	server, err := api.New()
	if err != nil {
		// Database DSNs and external-service errors can contain credentials.
		slog.Error("application_startup_failed", "error_type", fmt.Sprintf("%T", err), "reason", "initialization_failed_check_database_migrations_storage_and_gofile")
		os.Exit(1)
	}
	slog.Info("http_server_starting", "address", ":3000")
	if err = http.ListenAndServe(":3000", server); err != nil {
		slog.Error("http_server_failed", "error_type", fmt.Sprintf("%T", err))
		os.Exit(1)
	}
}
