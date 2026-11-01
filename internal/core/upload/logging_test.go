package upload

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestUploadProgressLoggingIsDebugOnly(t *testing.T) {
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })
	var output bytes.Buffer
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	if got := returnPercentageCompleted(25, 100); got != 25 {
		t.Fatalf("progress = %v, want 25", got)
	}
	if output.Len() != 0 {
		t.Fatal("upload progress logged at production INFO level")
	}

	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})))
	returnPercentageCompleted(25, 100)
	var event map[string]any
	if err := json.Unmarshal(output.Bytes(), &event); err != nil {
		t.Fatalf("decode structured event: %v", err)
	}
	if event["level"] != "DEBUG" || event["operation"] != "upload_progress" {
		t.Fatalf("unexpected progress event: %v", event)
	}
	for _, key := range []string{"name", "filename", "path", "user_id", "UserID"} {
		if _, exists := event[key]; exists {
			t.Errorf("progress event includes sensitive field %q", key)
		}
	}
}
