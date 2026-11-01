package notifications

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"html/template"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

type Config struct {
	BaseURL, Host, Port, Username, Password, From string
	ImplicitTLS, AllowInsecureLocalhost           bool
	Timeout                                       time.Duration
}

func localHost(host string) bool {
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}
func ConfigFromEnv() (Config, error) {
	c := Config{BaseURL: os.Getenv("APP_BASE_URL"), Host: os.Getenv("SMTP_HOST"), Port: os.Getenv("SMTP_PORT"), Username: os.Getenv("SMTP_USERNAME"), Password: os.Getenv("SMTP_PASSWORD"), From: os.Getenv("SMTP_FROM"), Timeout: 10 * time.Second}
	mode := os.Getenv("SMTP_TLS_MODE")
	if mode != "" && mode != "starttls" && mode != "implicit" {
		return c, errors.New("SMTP_TLS_MODE must be starttls or implicit")
	}
	c.ImplicitTLS = mode == "implicit"
	insecure := os.Getenv("SMTP_ALLOW_INSECURE_LOCALHOST")
	if insecure != "" && insecure != "true" && insecure != "false" {
		return c, errors.New("SMTP_ALLOW_INSECURE_LOCALHOST must be true or false")
	}
	c.AllowInsecureLocalhost = insecure == "true"
	if raw := os.Getenv("SMTP_TIMEOUT"); raw != "" {
		v, err := time.ParseDuration(raw)
		if err != nil {
			return c, errors.New("invalid SMTP_TIMEOUT")
		}
		c.Timeout = v
	}
	if c.Port == "" {
		if c.ImplicitTLS {
			c.Port = "465"
		} else {
			c.Port = "587"
		}
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
	if c.Host == "" || strings.ContainsAny(c.Host, "/\r\n\t @") {
		return errors.New("SMTP_HOST is required and must be a hostname")
	}
	n, err := strconv.Atoi(c.Port)
	if err != nil || n < 1 || n > 65535 {
		return errors.New("SMTP_PORT must be between 1 and 65535")
	}
	if c.AllowInsecureLocalhost && !localHost(c.Host) {
		return errors.New("insecure SMTP is allowed only on localhost or a literal loopback IP")
	}
	if (c.Username == "") != (c.Password == "") {
		return errors.New("SMTP_USERNAME and SMTP_PASSWORD must both be provided")
	}
	if c.Timeout < time.Second || c.Timeout > time.Minute {
		return errors.New("SMTP_TIMEOUT must be between 1s and 1m")
	}
	if _, err = singleAddress(c.From); err != nil {
		return errors.New("SMTP_FROM must be a valid email address")
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

func renderMail(from, to, kind, name, link string) ([]byte, error) {
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
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	plain := d.Title + "\n\n" + d.Intro + "\n\n"
	if d.Name != "" {
		plain += d.Name + "\n\n"
	}
	plain += d.Detail + "\n\n" + d.Label + ": " + d.Link + "\n\nSeedrlike · Less waiting. More doing.\nManage your email preferences in Seedrlike settings.\n"
	var html bytes.Buffer
	if err = emailHTML.Execute(&html, d); err != nil {
		return nil, err
	}
	for i, text := range []string{plain, html.String()} {
		typ := "text/plain; charset=UTF-8"
		if i == 1 {
			typ = "text/html; charset=UTF-8"
		}
		h := textproto.MIMEHeader{}
		h.Set("Content-Type", typ)
		h.Set("Content-Transfer-Encoding", "quoted-printable")
		p, e := mw.CreatePart(h)
		if e != nil {
			return nil, e
		}
		q := quotedprintable.NewWriter(p)
		if _, e = q.Write([]byte(text)); e != nil {
			return nil, e
		}
		if e = q.Close(); e != nil {
			return nil, e
		}
	}
	if err = mw.Close(); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	fmt.Fprintf(&out, "From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=%q\r\n\r\n", f.String(), t.String(), mime.QEncoding.Encode("utf-8", "Seedrlike · "+d.Title), time.Now().UTC().Format(time.RFC1123Z), mw.Boundary())
	out.Write(body.Bytes())
	return out.Bytes(), nil
}
func sendMail(ctx context.Context, c Config, to, kind, name, link string) error {
	// Defense in depth: no test binary may contact a real SMTP server, even if
	// a developer's shell happens to export production SMTP credentials.
	if testing.Testing() {
		return errors.New("SMTP delivery disabled in test binaries")
	}
	if err := c.Validate(); err != nil {
		return err
	}
	message, err := renderMail(c.From, to, kind, name, link)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	addr := net.JoinHostPort(c.Host, c.Port)
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		if err = conn.SetDeadline(deadline); err != nil {
			return err
		}
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	tlsCfg := &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}
	var transport net.Conn = conn
	if c.ImplicitTLS {
		secure := tls.Client(conn, tlsCfg)
		if err = secure.HandshakeContext(ctx); err != nil {
			return err
		}
		transport = secure
	}
	client, err := smtp.NewClient(transport, c.Host)
	if err != nil {
		return err
	}
	defer client.Close()
	if !c.ImplicitTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err = client.StartTLS(tlsCfg); err != nil {
				return err
			}
		} else if !c.AllowInsecureLocalhost {
			return errors.New("SMTP server must support STARTTLS")
		}
	}
	if c.Username != "" {
		if err = client.Auth(smtp.PlainAuth("", c.Username, c.Password, c.Host)); err != nil {
			return err
		}
	}
	from, _ := singleAddress(c.From)
	recipient, _ := singleAddress(to)
	if err = client.Mail(from.Address); err != nil {
		return err
	}
	if err = client.Rcpt(recipient.Address); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err = w.Write(message); err != nil {
		return err
	}
	if err = w.Close(); err != nil {
		return err
	}
	return client.Quit()
}
