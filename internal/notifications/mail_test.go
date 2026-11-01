package notifications

import (
	"context"
	"strings"
	"testing"
	"time"
)

func testConfig() Config {
	return Config{BaseURL: "https://seed.example", APIKey: "re_test_placeholder", From: "Seedrlike <hello@seed.example>", Timeout: 10 * time.Second}
}
func TestConfigValidation(t *testing.T) {
	good := testConfig()
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"", "//evil.example", "http://seed.example", "https://seed.example/path", "https://user:pass@seed.example", "https://seed.example?q=x", "https://seed.example#x", "https://seed.example:99999"} {
		t.Run(origin, func(t *testing.T) {
			c := good
			c.BaseURL = origin
			if c.Validate() == nil {
				t.Fatalf("accepted unsafe origin %q", origin)
			}
		})
	}
	c := good
	c.AllowInsecureLocalhost = true
	c.BaseURL = "http://localhost:8080"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.BaseURL = "http://remote.example"
	if c.Validate() == nil {
		t.Fatal("accepted insecure remote origin")
	}
	c = good
	c.From = "a@example.com\r\nBcc: victim@example.com"
	if c.Validate() == nil {
		t.Fatal("accepted header injection")
	}
	c = good
	c.APIKey = ""
	if c.Validate() == nil {
		t.Fatal("accepted missing api key")
	}
	c.APIKey = "re_key\r\nsecret"
	if c.Validate() == nil {
		t.Fatal("accepted key with whitespace")
	}
}
func TestConfigFromEnv(t *testing.T) {
	for k, v := range map[string]string{"APP_BASE_URL": "https://seed.example/", "RESEND_API_KEY": "re_test_placeholder", "RESEND_FROM": "hello@seed.example", "RESEND_TIMEOUT": "", "EMAIL_ALLOW_INSECURE_LOCALHOST": ""} {
		t.Setenv(k, v)
	}
	c, err := ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL != "https://seed.example" || c.Timeout != 10*time.Second {
		t.Fatal("unexpected config")
	}
	t.Setenv("RESEND_TIMEOUT", "0s")
	if _, err = ConfigFromEnv(); err == nil {
		t.Fatal("accepted unbounded timeout")
	}
	t.Setenv("RESEND_TIMEOUT", "invalid")
	if _, err = ConfigFromEnv(); err == nil {
		t.Fatal("accepted malformed timeout")
	}
	t.Setenv("RESEND_TIMEOUT", "1m")
	t.Setenv("EMAIL_ALLOW_INSECURE_LOCALHOST", "invalid")
	if _, err = ConfigFromEnv(); err == nil {
		t.Fatal("accepted invalid dev flag")
	}
}
func TestTemplatesEscape(t *testing.T) {
	for _, kind := range []string{"ready", "failed", "verify", "test"} {
		t.Run(kind, func(t *testing.T) {
			payload := `<img src=x onerror="alert(1)"> & report`
			msg, err := renderMail("hello@seed.example", "you@example.com", kind, payload, "https://seed.example/settings?a=1&b=2")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(msg.Text, payload) {
				t.Fatal("plaintext lost name")
			}
			if strings.Contains(msg.Html, "<img src=x") || !strings.Contains(msg.Html, "&lt;img") {
				t.Fatal("HTML did not escape hostile name")
			}
			if len(msg.To) != 1 || msg.To[0] != "you@example.com" || msg.Subject == "" {
				t.Fatal("mail fields missing")
			}
		})
	}
	if _, err := renderMail("a@example.com\nBcc: x@example.com", "b@example.com", "test", "", "https://example.com"); err == nil {
		t.Fatal("accepted header injection")
	}
}
func TestTokens(t *testing.T) {
	token, hash, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != 43 || len(hash) != 32 {
		t.Fatal("unexpected token dimensions")
	}
	token2, _, err := newToken()
	if err != nil || token2 == token {
		t.Fatal("token not random")
	}
	got, err := tokenHash(token)
	if err != nil || string(got) != string(hash) {
		t.Fatal("hash mismatch")
	}
	for _, bad := range []string{"", token + "=", "abc", strings.Repeat("!", 43)} {
		if _, err := tokenHash(bad); err == nil {
			t.Fatal("accepted malformed token")
		}
	}
	if strings.Contains(string(hash), token) {
		t.Fatal("token stored in cleartext")
	}
}
func TestNeverSendInTests(t *testing.T) {
	if err := sendMail(context.Background(), testConfig(), "a@example.com", "test", "", "https://seed.example"); err == nil || !strings.Contains(err.Error(), "test binaries") {
		t.Fatal("email test guard absent", err)
	}
}
func TestEmailValidation(t *testing.T) {
	for _, email := range []string{"victim@example.com\r\nBcc: x@example.com", "Display <a@example.com>", "invalid"} {
		if _, err := normalizeEmail(email); err == nil {
			t.Fatal("accepted invalid email", email)
		}
	}
	if email, err := normalizeEmail(" a@example.com "); err != nil || email != "a@example.com" {
		t.Fatal(email, err)
	}
}
