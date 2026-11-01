package notifications

import (
	"bytes"
	"context"
	"errors"
	"html/template"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/resend/resend-go/v4"
)

type Config struct {
	BaseURL, APIKey, From  string
	AllowInsecureLocalhost bool
	Timeout                time.Duration
}

func localHost(host string) bool {
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}
func ConfigFromEnv() (Config, error) {
	c := Config{BaseURL: os.Getenv("APP_BASE_URL"), APIKey: os.Getenv("RESEND_API_KEY"), From: os.Getenv("RESEND_FROM"), Timeout: 10 * time.Second}
	insecure := os.Getenv("EMAIL_ALLOW_INSECURE_LOCALHOST")
	if insecure != "" && insecure != "true" && insecure != "false" {
		return c, errors.New("EMAIL_ALLOW_INSECURE_LOCALHOST must be true or false")
	}
	c.AllowInsecureLocalhost = insecure == "true"
	if raw := os.Getenv("RESEND_TIMEOUT"); raw != "" {
		v, err := time.ParseDuration(raw)
		if err != nil {
			return c, errors.New("invalid RESEND_TIMEOUT")
		}
		c.Timeout = v
	}
	if err := c.Validate(); err != nil {
		return c, err
	}
	c.BaseURL = strings.TrimSuffix(c.BaseURL, "/")
	return c, nil
}
func (c Config) Validate() error {
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || (u.Path != "" && u.Path != "/") || u.RawPath != "" {
		return errors.New("APP_BASE_URL must be an absolute trusted origin without path, credentials, query, or fragment")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && c.AllowInsecureLocalhost && localHost(u.Hostname())) {
		return errors.New("APP_BASE_URL must use HTTPS (HTTP only for explicitly enabled localhost development)")
	}
	if strings.ContainsAny(c.BaseURL, "\r\n\t ") {
		return errors.New("invalid APP_BASE_URL")
	}
	if p := u.Port(); p != "" {
		n, e := strconv.Atoi(p)
		if e != nil || n < 1 || n > 65535 {
			return errors.New("invalid APP_BASE_URL port")
		}
	}
	if c.APIKey == "" || strings.ContainsAny(c.APIKey, " \r\n\t") {
		return errors.New("RESEND_API_KEY is required and must not contain whitespace")
	}
	if c.Timeout < time.Second || c.Timeout > time.Minute {
		return errors.New("RESEND_TIMEOUT must be between 1s and 1m")
	}
	if _, err = singleAddress(c.From); err != nil {
		return errors.New("RESEND_FROM must be a valid email address")
	}
	return nil
}
func singleAddress(s string) (*mail.Address, error) {
	if strings.ContainsAny(s, "\r\n") {
		return nil, errors.New("invalid mail header")
	}
	a, err := mail.ParseAddress(s)
	if err != nil {
		return nil, err
	}
	return a, nil
}

type emailContent struct{ Title, Intro, Name, Label, Link, Detail string }

func content(kind, name, link string) (emailContent, error) {
	d := emailContent{Name: name, Link: link}
	switch kind {
	case "ready":
		d.Title = "Your download is ready"
		d.Intro = "All done. Your files are ready when you are."
		d.Label = "Open your files"
		d.Detail = "Your download completed successfully. Sign in to Seedrlike to access your files."
	case "failed":
		d.Title = "A download needs your attention"
		d.Intro = "We couldn’t finish this download."
		d.Label = "Open Seedrlike"
		d.Detail = "This job did not finish. You can add its magnet link again to start a new attempt. If it failed during upload, ask the administrator to check for existing remote files first."
	case "verify":
		d.Title = "Make it official"
		d.Intro = "Confirm your email for Seedrlike notifications."
		d.Label = "Review & verify email"
		d.Detail = "This link expires in 30 minutes. You’ll confirm on the next page; opening this email or link does not verify your address. If you didn’t request this, ignore this email."
	case "test":
		d.Title = "You’re all connected"
		d.Intro = "A small check-in from Seedrlike."
		d.Label = "Notification settings"
		d.Detail = "Your test email arrived. We’ll send download updates according to your notification preferences."
	default:
		return d, errors.New("unknown email template")
	}
	return d, nil
}

var emailHTML = template.Must(template.New("mail").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>{{.Title}}</title><style>@media(max-width:600px){.card{padding:28px 20px!important}.wrap{padding:20px 10px!important}}</style></head><body style="margin:0;background:#eceff4;color:#2e3440;font-family:Arial,Helvetica,sans-serif"><div style="display:none;max-height:0;overflow:hidden">{{.Intro}}</div><table role="presentation" width="100%" cellspacing="0" cellpadding="0"><tr><td class="wrap" align="center" style="padding:48px 16px"><table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="max-width:560px"><tr><td style="padding:0 0 20px;font-weight:700;letter-spacing:1px;color:#4c566a">SEEDRLIKE <span style="color:#5e81ac">/</span> YOUR CLOUD, SIMPLIFIED</td></tr><tr><td class="card" style="background:#fff;border-radius:16px;border-top:5px solid #88c0d0;padding:40px"><p style="font-size:12px;letter-spacing:2px;font-weight:bold;color:#5e81ac;margin:0 0 16px">A LITTLE UPDATE</p><h1 style="font-size:30px;line-height:1.2;margin:0 0 18px">{{.Title}}</h1><p style="font-size:17px;line-height:1.6;color:#4c566a">{{.Intro}}</p>{{if .Name}}<div style="background:#eceff4;border-left:3px solid #81a1c1;padding:16px;margin:24px 0;overflow-wrap:anywhere;font-weight:600">{{.Name}}</div>{{end}}<p style="font-size:15px;line-height:1.7;color:#4c566a">{{.Detail}}</p><table role="presentation" cellspacing="0" cellpadding="0" style="margin-top:28px"><tr><td style="border-radius:8px;background:#5e81ac"><a href="{{.Link}}" style="display:inline-block;padding:14px 22px;color:white;text-decoration:none;font-size:15px;font-weight:bold">{{.Label}} &rarr;</a></td></tr></table><p style="font-size:12px;line-height:1.6;color:#4c566a;margin-top:24px">Button not working? Copy this link:<br><a href="{{.Link}}" style="color:#5e81ac;overflow-wrap:anywhere">{{.Link}}</a></p></td></tr><tr><td style="padding:24px 8px;font-size:12px;line-height:1.7;color:#4c566a">Less waiting. More doing.<br>Manage your email preferences in Seedrlike settings.<br>This is an automated message. Please don’t reply.</td></tr></table></td></tr></table></body></html>`))

// renderMail supplies both formats to Resend; the service constructs the MIME message.
func renderMail(from, to, kind, name, link string) (*resend.SendEmailRequest, error) {
	f, err := singleAddress(from)
	if err != nil {
		return nil, err
	}
	t, err := singleAddress(to)
	if err != nil {
		return nil, err
	}
	d, err := content(kind, name, link)
	if err != nil {
		return nil, err
	}
	plain := d.Title + "\n\n" + d.Intro + "\n\n"
	if d.Name != "" {
		plain += d.Name + "\n\n"
	}
	plain += d.Detail + "\n\n" + d.Label + ": " + d.Link + "\n\nSeedrlike · Less waiting. More doing.\nManage your email preferences in Seedrlike settings.\n"
	var html bytes.Buffer
	if err = emailHTML.Execute(&html, d); err != nil {
		return nil, err
	}
	return &resend.SendEmailRequest{From: f.String(), To: []string{t.Address}, Subject: "Seedrlike · " + d.Title, Text: plain, Html: html.String()}, nil
}

func sendMail(ctx context.Context, c Config, to, kind, name, link string) error {
	// No automated test may use credentials inherited from a developer's shell.
	if testing.Testing() {
		return errors.New("email delivery disabled in test binaries")
	}
	httpClient := &http.Client{Timeout: c.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return sendMailWithClient(ctx, c, to, kind, name, link, httpClient)
}

// Tests inject an in-memory HTTP transport, never a live Resend endpoint.
func sendMailWithClient(ctx context.Context, c Config, to, kind, name, link string, httpClient *http.Client) error {
	if err := c.Validate(); err != nil {
		return err
	}
	message, err := renderMail(c.From, to, kind, name, link)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	client := resend.NewCustomClient(httpClient, c.APIKey)
	// Ignore the SDK's optional RESEND_BASE_URL environment override so credentials
	// and verification links cannot be redirected to another service accidentally.
	client.BaseURL = &url.URL{Scheme: "https", Host: "api.resend.com", Path: "/"}
	response, err := client.Emails.SendWithContext(ctx, message)
	if err != nil {
		return errors.New("Resend email submission failed")
	}
	if response == nil || strings.TrimSpace(response.Id) == "" {
		return errors.New("Resend email submission was not confirmed")
	}
	return nil
}
