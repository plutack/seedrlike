package components

import (
	"bytes"
	"context"
	"html"
	"strings"
	"testing"

	"github.com/plutack/seedrlike/internal/notifications"
)

func TestNotificationSettingsConciseActions(t *testing.T) {
	for _, tc := range []struct {
		name          string
		settings      notifications.Settings
		action, label string
	}{
		{"verified", notifications.Settings{Email: "owner@example.com", Verified: true, Completed: true}, "save", "Save"},
		{"unverified", notifications.Settings{Email: "owner@example.com"}, "verify", "Save &amp; verify"},
		{"new", notifications.Settings{}, "save", "Save"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := NotificationSettings(tc.settings, "csrf-token", "", false).Render(context.Background(), &out); err != nil {
				t.Fatal(err)
			}
			body := out.String()
			for _, want := range []string{`method="post" action="/settings"`, `name="csrf" value="csrf-token"`, `name="action" value="` + tc.action + `"`, `>` + tc.label + `</button>`, `name="action" value="unset"`, `formnovalidate`, `x-model="email"`} {
				if !strings.Contains(body, want) {
					t.Errorf("missing %q", want)
				}
			}
			if strings.Count(body, `<button`) != 2 {
				t.Error("expected one primary action and Unset")
			}
			for _, unwanted := range []string{"Where should", "Less checking", "email-help", "Ready to receive", "Check your inbox after", "Keep me in the loop", "/settings/test", "test email", "30 minutes", "per minute", "Save &amp; send verification"} {
				if strings.Contains(body, unwanted) {
					t.Errorf("unexpected clutter %q", unwanted)
				}
			}
			// The exact same state drives status, submit action and label. Reverting an edit
			// restores verification, matching Save's whitespace-normalized email comparison.
			decoded := html.UnescapeString(body)
			for _, want := range []string{"this.originallyVerified && this.email.trim() === this.originalEmail", `x-show="verified"`, `x-show="email.trim() && !verified"`, `x-bind:value="email.trim() && !verified ? 'verify' : 'save'"`, `x-text="email.trim() && !verified ? 'Save & verify' : 'Save'"`} {
				if !strings.Contains(decoded, want) {
					t.Errorf("missing reactive verification state %q", want)
				}
			}
		})
	}
}

func TestNotificationSettingsEscapesEmailState(t *testing.T) {
	var out bytes.Buffer
	email := `"><script>alert(1)</script>`
	if err := NotificationSettings(notifications.Settings{Email: email}, "csrf", "", false).Render(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), email) || strings.Contains(out.String(), "<script>alert(1)</script>") {
		t.Fatal("email state is not escaped")
	}
	if !strings.Contains(out.String(), "&lt;script&gt;") {
		t.Fatal("missing escaped email")
	}
}
