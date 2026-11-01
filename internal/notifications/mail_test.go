package notifications

import (
	"context"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"testing"
	"time"
)

func testConfig() Config {
	return Config{BaseURL: "https://seed.example", Host: "smtp.example", Port: "587", From: "Seedrlike <hello@seed.example>", Timeout: 10 * time.Second}
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
	for _, host := range []string{"smtp.example", "127.0.0.1.evil.example", "localhost.evil.example"} {
		c := good
		c.AllowInsecureLocalhost = true
		c.Host = host
		if c.Validate() == nil {
			t.Fatal("accepted remote plaintext SMTP", host)
		}
	}
	c := good
	c.AllowInsecureLocalhost = true
	c.Host = "127.0.0.1"
	c.BaseURL = "http://localhost:8080"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c = good
	c.From = "a@example.com\r\nBcc: victim@example.com"
	if c.Validate() == nil {
		t.Fatal("accepted header injection")
	}
	c = good
	c.Username = "user"
	if c.Validate() == nil {
		t.Fatal("accepted unpaired credentials")
	}
}
func TestConfigFromEnv(t *testing.T) {
	for k, v := range map[string]string{"APP_BASE_URL": "https://seed.example/", "SMTP_HOST": "smtp.example", "SMTP_FROM": "hello@seed.example", "SMTP_PORT": "", "SMTP_USERNAME": "", "SMTP_PASSWORD": "", "SMTP_TLS_MODE": "", "SMTP_TIMEOUT": "", "SMTP_ALLOW_INSECURE_LOCALHOST": ""} {
		t.Setenv(k, v)
	}
	c, err := ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL != "https://seed.example" || c.Port != "587" {
		t.Fatalf("unexpected config %#v", c)
	}
	t.Setenv("SMTP_TLS_MODE", "implicit")
	c, err = ConfigFromEnv()
	if err != nil || c.Port != "465" || !c.ImplicitTLS {
		t.Fatal(c, err)
	}
	t.Setenv("SMTP_TLS_MODE", "none")
	if _, err = ConfigFromEnv(); err == nil {
		t.Fatal("accepted disabled TLS")
	}
	t.Setenv("SMTP_TLS_MODE", "starttls")
	t.Setenv("SMTP_TIMEOUT", "0s")
	if _, err = ConfigFromEnv(); err == nil {
		t.Fatal("accepted unbounded timeout")
	}
}
func TestMultipartTemplatesEscape(t *testing.T) {
	for _, kind := range []string{"ready", "failed", "verify", "test"} {
		t.Run(kind, func(t *testing.T) {
			payload := `<img src=x onerror="alert(1)"> & report`
			raw, err := renderMail("hello@seed.example", "you@example.com", kind, payload, "https://seed.example/settings?a=1&b=2")
			if err != nil {
				t.Fatal(err)
			}
			msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
			if err != nil {
				t.Fatal(err)
			}
			mt, p, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
			if err != nil || mt != "multipart/alternative" {
				t.Fatal(mt, err)
			}
			parts := multipart.NewReader(msg.Body, p["boundary"])
			for i := 0; i < 2; i++ {
				part, err := parts.NextRawPart()
				if err != nil {
					t.Fatal(err)
				}
				b, err := io.ReadAll(quotedprintable.NewReader(part))
				if err != nil {
					t.Fatal(err)
				}
				text := string(b)
				if i == 0 && !strings.Contains(text, payload) {
					t.Fatal("plaintext lost name")
				}
				if i == 1 && (strings.Contains(text, "<img src=x") || !strings.Contains(text, "&lt;img")) {
					t.Fatal("HTML did not escape hostile name")
				}
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
		t.Fatal("SMTP test guard absent", err)
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
