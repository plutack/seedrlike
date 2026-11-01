# Email notifications

The application automatically applies pending embedded Goose/MySQL migrations at startup, including the notifications migration, before starting workers or HTTP routes. No manual migration or separate Goose CLI is required. Existing migration history is respected, and a migration failure stops startup. Existing users start opted out, with no email address. Database connections use `parseTime=true`.

## Routes and integration

The authenticated settings routes are already registered:

- `GET /settings`: view notification settings.
- `POST /settings`: update the owner's address and event preferences.
- `GET /settings/verify`: render the verification confirmation form.
- `POST /settings/verify`: confirm verification for the authenticated owner.
- `POST /settings/test`: send a test to the owner's currently verified address.

The authenticated navigation links to `/settings`. The shared settings layout can wrap other settings pages; no manual route or template integration is needed to enable notifications.

## Worker hook

Use one `notifications.New(db)` service and call the hook once after the job reaches a terminal state, with its actual owner ID:

```go
ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
defer cancel()
if err := notifier.Notify(ctx, job.UserID, job.Name, "completed"); err != nil {
    slog.WarnContext(ctx, "download notification failed", "error_type", fmt.Sprintf("%T", err))
}
```

The example uses Go `log/slog` and `fmt`; it logs the error type rather than potentially sensitive provider error text. Use `"failed"` for a terminal failure. Do not let an email failure change job status. Guests, disabled event preferences, and unverified recipients are skipped. The worker should invoke this only on the terminal transition, not on every progress update: this service does not provide an outbox or duplicate suppression. Ready notifications occur only after successful remote upload. Do not expose raw internal error strings or exception traces in the download name.

## Environment

Email delivery uses the official Resend Go SDK v4 (`github.com/resend/resend-go/v4`) and the Resend HTTPS API, not SMTP.

- `APP_BASE_URL`: required administrator-controlled absolute HTTPS origin, for example `https://seed.example`. No credentials, path other than `/`, query, or fragment. Never inferred from request headers.
- `RESEND_API_KEY`: required Resend API key, with no whitespace. Keep it secret and authorize it to send from the configured domain.
- `RESEND_FROM`: required mailbox on a Resend-verified domain, optionally with a display name, for example `Seedrlike <notifications@example.com>`.
- `RESEND_TIMEOUT`: optional request duration, default `10s`, allowed range `1s`–`1m`.
- `EMAIL_ALLOW_INSECURE_LOCALHOST`: optional boolean, for development only. Explicit `true` permits an HTTP `APP_BASE_URL` only on `localhost` or a literal loopback IP. It does not allow an insecure Resend endpoint or disable certificate verification. The Resend API endpoint stays fixed at `https://api.resend.com`.

Old `SMTP_*` environment variables are no longer used; replace them with the Resend settings above. No secrets or local environment values are modified by this package. Invalid or missing delivery configuration is reported when email delivery is requested, not when users view or save settings.

## Resend setup

```env
APP_BASE_URL=https://seedrlike.yourdomain.com
RESEND_API_KEY=your-resend-api-key
RESEND_FROM="Seedrlike <notifications@yourdomain.com>"
RESEND_TIMEOUT=10s
```

Add and verify a domain you own in Resend using its required DNS records; Cloudflare can continue managing your DNS. Create a Resend API key authorized to send from that domain and set `RESEND_FROM` to a mailbox on the verified domain. A display name is optional. Set `APP_BASE_URL` to the real public HTTPS application origin and leave `EMAIL_ALLOW_INSECURE_LOCALHOST` unset in production. No Worker, relay, or configurable provider endpoint is required.

Resend's account quotas and rate limits apply. Delivery can be inspected in the Resend dashboard. The app's templates, recipient verification flow, API routes, and owner checks are unchanged by the provider transport change.

References: [Send emails with Go](https://resend.com/docs/send-with-go) and [Resend verified domains](https://resend.com/docs/dashboard/domains/introduction).

## Security and behavior

Verification tokens contain 32 random bytes, have a 30-minute lifetime, and are stored only as SHA-256 hashes. Verification GET only renders a confirmation form. POST consumes a token atomically, constrained to the authenticated owner. Changing or clearing an email resets verification and invalidates the pending token. Updating event preferences alone retains verification.

Verification and test requests each have their own persisted 60-second owner-level cooldown; editing the address does not reset them. Delivery failures retain cooldown reservations. Tests can only target a currently verified address; download and test sends lock the owner row during the bounded Resend API request to prevent concurrent email changes from racing delivery. Settings mutations require a random same-site double-submit CSRF token. Token pages are `no-store` with `no-referrer`.

Messages supply plaintext and responsive Nord-branded HTML to the Resend API. Names, addresses, and link values are escaped appropriately; header injection is rejected. Actual email sending is unconditionally blocked in Go test binaries. An injected fake HTTP transport can exercise SDK request construction and inspect the API payload offline without contacting Resend.

## Scoped verification

```sh
GOCACHE="$PWD/.cache/go-build" go test -race ./internal/notifications
GOCACHE="$PWD/.cache/go-build" go test ./internal/api/handlers -run 'TestNotificationEndpointsRequireOwner|TestVerificationGetOnlyConfirmsAndEscapes|TestMutationsRequireCSRF|TestSettingsRejectWrongMethod'
```

Tests need no live provider or database. Live MySQL migration and real Resend delivery should be checked separately in staging, outside Go test binaries.
