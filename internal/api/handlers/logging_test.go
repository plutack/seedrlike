package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestQueueFailureLoggingRedactsRawError(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	secret := "postgres://user:password@host/database private@example.com magnet:?secret /private/file-name"
	message := publicQueueError(errors.New(secret))
	if strings.Contains(message, secret) || strings.Contains(output.String(), secret) {
		t.Fatal("queue failure exposed raw error contents")
	}
	var event map[string]any
	if err := json.Unmarshal(output.Bytes(), &event); err != nil {
		t.Fatalf("decode structured event: %v", err)
	}
	for key, want := range map[string]string{
		"level":      "ERROR",
		"operation":  "enqueue_download",
		"reason":     "queue_unavailable",
		"error_type": "*errors.errorString",
	} {
		if event[key] != want {
			t.Errorf("%s = %v, want %q", key, event[key], want)
		}
	}
}

func TestExpectedQueueRejectionDoesNotLog(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	for _, reason := range []string{"invalid magnet link", "select at least one file", "invalid file selection", "download queue full", "torrent already active"} {
		if got := publicQueueError(errors.New(reason)); got != reason {
			t.Errorf("queue response = %q, want %q", got, reason)
		}
	}
	if output.Len() != 0 {
		t.Fatal("expected queue rejection generated a production error log")
	}
}
