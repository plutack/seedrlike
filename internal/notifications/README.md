# Email notifications integration

Apply migration `internal/database/migrations/20261101000200_notifications.sql` with the existing Goose/MySQL migration runner. Existing users start opted out, with no email address. The connection must use `parseTime=true` (as the current server does).

## Routes

Register under the existing authentication middleware:

```go
settings := handlers.NewSettingsHandler(s.db)
apiRouter.HandleFunc("/settings", settings.Settings).Methods("GET")
apiRouter.HandleFunc("/settings", settings.Save).Methods("POST")
apiRouter.HandleFunc("/settings/verify", settings.Verify).Methods("GET", "POST")
apiRouter.HandleFunc("/settings/test", settings.Test).Methods("POST")
```

Add an authenticated navigation link to `/settings`. The shared layout is `layouts.SettingsPage(title string, loggedIn bool, content templ.Component)` and can wrap other settings pages. Generate the two new templ files and rebuild Tailwind after adding templates.

## Worker hook

Create one `notifications.New(db)` service and call the hook once after the job reaches a terminal state, with its actual owner ID:

```go
ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
defer cancel()
if err := notifier.Notify(ctx, job.UserID, job.Name, "completed"); err != nil {
    log.Printf("download notification failed: %v", err)
}
```

Use `"failed"` for a terminal failure. Do not let an email failure change job status. Guests, disabled event preferences, and unverified recipients are skipped. The worker should invoke this only on the terminal transition, not on every progress update: this service does not provide an outbox or duplicate suppression. Do not expose raw internal error strings or exception traces in the download name.

## Environment

- `APP_BASE_URL`: administrator-controlled absolute HTTPS origin, for example `https://seed.example`. No credentials, path other than `/`, query, or fragment. Never inferred from request headers.
- `SMTP_HOST`: required SMTP server hostname (or literal IP).
- `SMTP_FROM`: required mailbox, optionally with a display name.
- `SMTP_PORT`: defaults to `587`, or `465` in implicit TLS mode.
- `SMTP_TLS_MODE`: `starttls` (default, server support required) or `implicit`. TLS 1.2 minimum; certificate validation is always enabled.
- `SMTP_USERNAME` / `SMTP_PASSWORD`: optional paired authentication credentials.
- `SMTP_TIMEOUT`: duration, default `10s`, allowed range `1s`–`1m`. Covers connection, TLS, and SMTP exchange.
- `SMTP_ALLOW_INSECURE_LOCALHOST`: explicit `true` permits no-TLS SMTP only for `localhost` or literal loopback IP and permits HTTP `APP_BASE_URL` only on localhost/loopback. Remote SMTP hosts are rejected when this flag is enabled. No certificate verification bypass exists.

No secrets or development `.envrc` values are modified by this package. An invalid/missing SMTP configuration is reported when email delivery is requested, not when users view or save settings.

## Google SMTP

The app sends through Google's SMTP endpoint directly; no Worker, relay, or provider adapter is required. Configure:

```env
APP_BASE_URL=https://seedrlike.yourdomain.com
SMTP_HOST=smtp.gmail.com
SMTP_PORT=587
SMTP_TLS_MODE=starttls
SMTP_FROM="Seedrlike <your-account@gmail.com>"
SMTP_USERNAME=your-account@gmail.com
SMTP_PASSWORD=your-google-app-password
SMTP_TIMEOUT=10s
```

Enable 2-Step Verification for the Google account and create an App Password where permitted. Use the full account email as `SMTP_USERNAME` and the App Password (without display spaces), not the normal account password, as `SMTP_PASSWORD`. Keep `SMTP_FROM` on that account or an authorized **Send mail as** alias. Set `APP_BASE_URL` to the real public HTTPS application origin and leave `SMTP_ALLOW_INSECURE_LOCALHOST` unset.

App Password availability depends on account/security and Workspace administrator policies; if unavailable, this password-based mailer cannot use that account without another authentication integration. This implementation does not implement OAuth2. Gmail/Workspace sending limits and anti-abuse restrictions still apply, so this is intended for modest notification volume, not bulk mail. Cloudflare may continue to manage the application's DNS/proxy independently.

References: [Google App Passwords](https://support.google.com/accounts/answer/185833) and [Google Workspace SMTP setup](https://support.google.com/a/answer/176600).

## Security and behavior

Verification tokens contain 32 random bytes, have a 30-minute lifetime, and are stored only as SHA-256 hashes. Verification GET only renders a confirmation form. POST consumes a token atomically, constrained to the authenticated owner. Changing or clearing an email resets verification and invalidates the pending token. Updating event preferences alone retains verification.

Verification and test requests each have their own persisted 60-second owner-level cooldown; editing the address does not reset them. Delivery failures retain cooldown reservations. Tests can only target a currently verified address; download and test sends lock the owner row during the bounded SMTP exchange to prevent concurrent email changes from racing delivery. Settings mutations require a random same-site double-submit CSRF token. Token pages are `no-store` with `no-referrer`.

Messages contain quoted-printable multipart plaintext and responsive Nord-branded HTML. Names, addresses, and link values are escaped appropriately; header injection is rejected. SMTP network delivery is unconditionally disabled in Go test binaries.

## Scoped verification

```sh
GOCACHE="$PWD/.cache/go-build" go test -race ./internal/notifications
GOCACHE="$PWD/.cache/go-build" go test ./internal/api/handlers -run 'TestNotificationEndpointsRequireOwner|TestVerificationGetOnlyConfirmsAndEscapes|TestMutationsRequireCSRF|TestSettingsRejectWrongMethod'
```

Tests use no SMTP server or live database. Live MySQL migration and SMTP-provider delivery should be checked separately in staging.
