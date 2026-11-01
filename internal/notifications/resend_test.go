package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/resend/resend-go/v4"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func apiResponse(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestResendSDKPayloadAndDeadline(t *testing.T) {
	c := testConfig()
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "POST" || r.URL.String() != "https://api.resend.com/emails" {
			t.Fatalf("unexpected endpoint %s", r.URL)
		}
		if r.Header.Get("Authorization") != "Bearer "+c.APIKey {
			t.Fatal("missing auth")
		}
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > c.Timeout {
			t.Fatal("request not bounded")
		}
		var msg resend.SendEmailRequest
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			t.Fatal(err)
		}
		from, parseErr := mail.ParseAddress(msg.From)
		if parseErr != nil || from.Address != "hello@seed.example" || from.Name != "Seedrlike" {
			t.Fatal("invalid sender")
		}
		if len(msg.To) != 1 || msg.To[0] != "recipient@example.com" || msg.Html == "" || msg.Text == "" || msg.Subject == "" {
			t.Fatal("incomplete payload")
		}
		return apiResponse(200, `{"id":"email-id"}`), nil
	})}
	if err := sendMailWithClient(context.Background(), c, "recipient@example.com", "test", "", "https://seed.example/settings", client); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("unexpected retries %d", calls)
	}
}
func TestResendFailureRedactsProviderResponse(t *testing.T) {
	for _, tc := range []struct {
		name string
		code int
		body string
		err  error
	}{
		{"unauthorized", 401, `{"name":"validation_error","message":"SECRET recipient@example.com"}`, nil},
		{"rate limit", 429, `{"message":"SECRET"}`, nil},
		{"server error", 500, `{"message":"SECRET"}`, nil},
		{"malformed", 200, `not-json SECRET`, nil},
		{"missing id", 200, `{}`, nil},
		{"blank id", 200, `{"id":" "}`, nil},
		{"transport", 0, "", errors.New("SECRET api token")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				if tc.err != nil {
					return nil, tc.err
				}
				return apiResponse(tc.code, tc.body), nil
			})}
			err := sendMailWithClient(context.Background(), testConfig(), "recipient@example.com", "verify", "", "https://seed.example/settings/verify?token=SECRET", client)
			if err == nil || strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "recipient@example.com") {
				t.Fatalf("unsafe result %v", err)
			}
			if calls != 1 {
				t.Fatalf("retries risk duplicate delivery: %d", calls)
			}
		})
	}
}
func TestResendContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if !errors.Is(r.Context().Err(), context.Canceled) {
			t.Error("context cancellation lost")
		}
		return nil, r.Context().Err()
	})}
	if err := sendMailWithClient(ctx, testConfig(), "recipient@example.com", "test", "", "https://seed.example", client); err == nil {
		t.Fatal("cancelled send succeeded")
	}
}
