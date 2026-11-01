// Package notifications provides opt-in, owner-scoped email notifications.
package notifications

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
)

var (
	ErrRateLimited  = errors.New("please wait a minute before sending another email")
	ErrUnverified   = errors.New("verify your notification email first")
	ErrInvalidToken = errors.New("verification link is invalid or expired; request a new one")
)

type Settings struct {
	Email     string
	Verified  bool
	Completed bool
	Failed    bool
}

type Service struct{ db *sql.DB }

func New(db *sql.DB) *Service { return &Service{db: db} }

func normalizeEmail(email string) (string, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return "", nil
	}
	a, err := mail.ParseAddress(email)
	if err != nil || a.Address != email || len(email) > 254 || strings.ContainsAny(email, "\r\n") {
		return "", errors.New("enter a valid email address without a display name")
	}
	return email, nil
}
func (s *Service) Settings(ctx context.Context, userID string) (Settings, error) {
	var v Settings
	var verified sql.NullTime
	if userID == "" {
		return v, errors.New("authentication required")
	}
	err := s.db.QueryRowContext(ctx, `SELECT Email, Verified_At, Notify_Completed, Notify_Failed FROM Notification_Settings WHERE User_ID=?`, userID).Scan(&v.Email, &verified, &v.Completed, &v.Failed)
	if errors.Is(err, sql.ErrNoRows) {
		return v, nil
	}
	v.Verified = verified.Valid
	return v, err
}
func (s *Service) Save(ctx context.Context, userID, email string, completed, failed bool) error {
	if userID == "" {
		return errors.New("authentication required")
	}
	email, err := normalizeEmail(email)
	if err != nil {
		return err
	}
	// Compare before assigning Email; unchanged addresses retain verification. Persisted
	// cooldowns deliberately survive address changes so edits cannot bypass limits.
	_, err = s.db.ExecContext(ctx, `INSERT INTO Notification_Settings (User_ID,Email,Notify_Completed,Notify_Failed) VALUES (?,?,?,?) ON DUPLICATE KEY UPDATE Verified_At=IF(BINARY Email=BINARY VALUES(Email),Verified_At,NULL), Verification_Hash=IF(BINARY Email=BINARY VALUES(Email),Verification_Hash,NULL), Verification_Expires_At=IF(BINARY Email=BINARY VALUES(Email),Verification_Expires_At,NULL), Email=VALUES(Email),Notify_Completed=VALUES(Notify_Completed),Notify_Failed=VALUES(Notify_Failed)`, userID, email, completed, failed)
	return err
}
func newToken() (string, []byte, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	hash := sha256.Sum256([]byte(token))
	return token, hash[:], nil
}
func tokenHash(token string) ([]byte, error) {
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(b) != 32 || base64.RawURLEncoding.EncodeToString(b) != token {
		return nil, ErrInvalidToken
	}
	h := sha256.Sum256([]byte(token))
	return h[:], nil
}
func (s *Service) RequestVerification(ctx context.Context, userID string) error {
	if userID == "" {
		return errors.New("authentication required")
	}
	cfg, err := ConfigFromEnv()
	if err != nil {
		return err
	}
	token, hash, err := newToken()
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var email string
	var last sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT Email,Last_Verification_At FROM Notification_Settings WHERE User_ID=? FOR UPDATE`, userID).Scan(&email, &last)
	if err != nil {
		return err
	}
	if email == "" {
		return errors.New("save an email address first")
	}
	now := time.Now().UTC()
	if last.Valid && now.Sub(last.Time) < time.Minute {
		return ErrRateLimited
	}
	_, err = tx.ExecContext(ctx, `UPDATE Notification_Settings SET Verification_Hash=?,Verification_Expires_At=?,Last_Verification_At=? WHERE User_ID=?`, hash, now.Add(30*time.Minute), now, userID)
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	// Only a hash is persisted. A delivery failure keeps the cooldown to prevent abuse.
	return sendMail(ctx, cfg, email, "verify", "", cfg.BaseURL+"/settings/verify?token="+token)
}
func (s *Service) Verify(ctx context.Context, userID, token string) error {
	if userID == "" {
		return errors.New("authentication required")
	}
	hash, err := tokenHash(token)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE Notification_Settings SET Verified_At=?,Verification_Hash=NULL,Verification_Expires_At=NULL WHERE User_ID=? AND Verification_Hash=? AND Verification_Expires_At>? AND Email<>''`, time.Now().UTC(), userID, hash, time.Now().UTC())
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrInvalidToken
	}
	return nil
}
func (s *Service) Test(ctx context.Context, userID string) error {
	if userID == "" {
		return errors.New("authentication required")
	}
	cfg, err := ConfigFromEnv()
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var email string
	var verified, last sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT Email,Verified_At,Last_Test_At FROM Notification_Settings WHERE User_ID=? FOR UPDATE`, userID).Scan(&email, &verified, &last)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUnverified
	}
	if err != nil {
		return err
	}
	if !verified.Valid || email == "" {
		return ErrUnverified
	}
	now := time.Now().UTC()
	if last.Valid && now.Sub(last.Time) < time.Minute {
		return ErrRateLimited
	}
	if _, err = tx.ExecContext(ctx, `UPDATE Notification_Settings SET Last_Test_At=? WHERE User_ID=?`, now, userID); err != nil {
		return err
	}
	// Persist the rate-limit reservation before any network activity, including
	// cancellation or transport failures. Re-lock and re-check the address before
	// delivery so a concurrent email change cannot receive an unverified test.
	if err = tx.Commit(); err != nil {
		return err
	}
	delivery, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer delivery.Rollback()
	var currentEmail string
	var currentVerified sql.NullTime
	if err = delivery.QueryRowContext(ctx, `SELECT Email,Verified_At FROM Notification_Settings WHERE User_ID=? FOR UPDATE`, userID).Scan(&currentEmail, &currentVerified); err != nil {
		return err
	}
	if !currentVerified.Valid || currentEmail != email {
		return ErrUnverified
	}
	return sendMail(ctx, cfg, email, "test", "", cfg.BaseURL+"/settings")
}

// Notify is the worker hook. It ignores guests, disabled events, and unverified
// addresses. Only completed and failed statuses are supported; delivery errors
// must be logged by the caller, never used to fail an otherwise completed task.
func (s *Service) Notify(ctx context.Context, userID, name, status string) error {
	if userID == "" {
		return nil
	}
	if status != "completed" && status != "failed" {
		return fmt.Errorf("unsupported notification status %q", status)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var email string
	var verified sql.NullTime
	var completed, failed bool
	err = tx.QueryRowContext(ctx, `SELECT Email,Verified_At,Notify_Completed,Notify_Failed FROM Notification_Settings WHERE User_ID=? FOR UPDATE`, userID).Scan(&email, &verified, &completed, &failed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if !verified.Valid || email == "" || (status == "completed" && !completed) || (status == "failed" && !failed) {
		return nil
	}
	cfg, err := ConfigFromEnv()
	if err != nil {
		return err
	}
	kind := "ready"
	if status == "failed" {
		kind = "failed"
	}
	if err = sendMail(ctx, cfg, email, kind, name, cfg.BaseURL+"/"); err != nil {
		return err
	}
	return tx.Commit()
}
