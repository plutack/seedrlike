// Package migrations embeds schema migrations in the application binary.
package migrations

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"fmt"
	"log/slog"
	"time"

	"github.com/pressly/goose/v3"
)

//go:embed *.sql
var Files embed.FS

// Up applies pending migrations using the same history as the Goose CLI.
// No down migrations or out-of-order migrations are run automatically.
func Up(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("migration database is required")
	}
	provider, err := goose.NewProvider(goose.DialectMySQL, db, Files, goose.WithDisableGlobalRegistry(true), goose.WithSessionLocker(mysqlLocker{}))
	if err != nil {
		return fmt.Errorf("initialize migrations: %w", err)
	}
	slog.Info("database_migrations_started")
	results, err := provider.Up(ctx)
	if err != nil {
		slog.Error("database_migrations_failed", "error_type", fmt.Sprintf("%T", err), "reason", "inspect_schema_and_goose_history_before_restart")
		return fmt.Errorf("apply startup migrations: %w (MySQL DDL may be partially applied; inspect schema and goose_db_version before restarting)", err)
	}
	for _, result := range results {
		slog.Info("database_migration_applied", "migration", result.Source.Path, "version", result.Source.Version)
	}
	if len(results) == 0 {
		slog.Info("database_schema_current")
	}
	return nil
}

// MySQL named locks are connection-scoped. Goose holds this same connection
// through initialization, schema changes, and version-history updates.
type mysqlLocker struct{}

func lockName(database string) string {
	return fmt.Sprintf("seedrlike:migrations:%x", sha256.Sum256([]byte(database)))[:64]
}
func (mysqlLocker) SessionLock(ctx context.Context, conn *sql.Conn) error {
	var database sql.NullString
	if err := conn.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&database); err != nil {
		return err
	}
	if !database.Valid || database.String == "" {
		return fmt.Errorf("a database must be selected in GOOSE_DBSTRING")
	}
	var acquired sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, ?)", lockName(database.String), 60).Scan(&acquired); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	if !acquired.Valid || acquired.Int64 != 1 {
		return fmt.Errorf("could not acquire migration lock within 60 seconds")
	}
	return nil
}
func (mysqlLocker) SessionUnlock(_ context.Context, conn *sql.Conn) error {
	// Use a fresh deadline even if migration context was cancelled.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var database sql.NullString
	if err := conn.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&database); err != nil {
		return err
	}
	var released sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT RELEASE_LOCK(?)", lockName(database.String)).Scan(&released); err != nil {
		return err
	}
	if !released.Valid || released.Int64 != 1 {
		return fmt.Errorf("migration lock was not released")
	}
	return nil
}
