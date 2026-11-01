package components

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/plutack/seedrlike/internal/notifications"
)

func TestNotificationSettingsCompactStates(t *testing.T) {
	for _, tc := range []struct {
		name   string
		s      notifications.Settings
		status string
		test   bool
	}{
		{"empty", notifications.Settings{}, "Off", false},
		{"unverified", notifications.Settings{Email: "updates@example.com"}, "Unverified", false},
		{"verified", notifications.Settings{Email: "updates@example.com", Verified: true}, "Verified", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b bytes.Buffer
			if err := NotificationSettings(tc.s, "csrf-token", "", false).Render(context.Background(), &b); err != nil {
				t.Fatal(err)
			}
			html := b.String()
			for _, old := range []string{"Where should we send updates?", "Separate from your username", "Not connected", "Add an email to get started", "Make sure it reaches you", "Keep me in the loop"} {
				if strings.Contains(html, old) {
					t.Fatalf("redundant copy remains %q", old)
				}
			}
			for _, want := range []string{tc.status, `name="csrf" value="csrf-token"`, `name="email"`, `name="completed"`, `name="failed"`, `x-model="email"`, `savedEmail`, `Unsaved`, `Verify email`} {
				if !strings.Contains(html, want) {
					t.Fatalf("missing %q", want)
				}
			}
			if strings.Contains(html, `action="/settings/test"`) != tc.test {
				t.Fatal("test email must only be shown for verified recipients")
			}
		})
	}
}
