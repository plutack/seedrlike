// Package observability configures container-friendly logs and request metadata.
package observability

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
)

type requestIDKey struct{}

func RequestID(ctx context.Context) string { v, _ := ctx.Value(requestIDKey{}).(string); return v }

// Configure directs application and standard-library log output to one JSON sink.
func Configure(out io.Writer, level string) error {
	var l slog.Level
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "", "info":
		l = slog.LevelInfo
	case "debug":
		l = slog.LevelDebug
	case "warn", "warning":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		return fmt.Errorf("LOG_LEVEL must be debug, info, warn, or error")
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(out, &slog.HandlerOptions{Level: l})))
	return nil
}
