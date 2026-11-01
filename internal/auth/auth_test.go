package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestTokenLifetimes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		duration time.Duration
		generate func(string) (string, error)
	}{
		{"default", 24 * time.Hour, GenerateToken},
		{"remember", 30 * 24 * time.Hour, func(id string) (string, error) { return GenerateTokenWithDuration(id, RememberTokenDuration) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := time.Now().Truncate(time.Second)
			token, err := tc.generate("user-123")
			if err != nil {
				t.Fatal(err)
			}
			claims, err := ValidateToken(token)
			if err != nil {
				t.Fatal(err)
			}
			if claims.UserID != "user-123" || claims.IssuedAt == nil || claims.ExpiresAt == nil {
				t.Fatalf("missing claims: %+v", claims)
			}
			if claims.IssuedAt.Before(before) || claims.IssuedAt.After(time.Now()) {
				t.Fatal("unexpected issued-at")
			}
			if got := claims.ExpiresAt.Sub(claims.IssuedAt.Time); got != tc.duration {
				t.Fatalf("lifetime %v, want %v", got, tc.duration)
			}
			again, err := ValidateToken(token)
			if err != nil || !again.ExpiresAt.Equal(claims.ExpiresAt.Time) {
				t.Fatal("validation changed expiry", err)
			}
		})
	}
}

func TestValidateTokenRequiresValidExpiryAndSignature(t *testing.T) {
	for _, tc := range []struct {
		name   string
		claims Claims
		method jwt.SigningMethod
		key    []byte
		want   error
	}{
		{"missing expiry", Claims{UserID: "u"}, jwt.SigningMethodHS256, jwtKey, jwt.ErrTokenRequiredClaimMissing},
		{"expired", Claims{RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour))}}, jwt.SigningMethodHS256, jwtKey, jwt.ErrTokenExpired},
		{"future issued at", Claims{RegisteredClaims: jwt.RegisteredClaims{IssuedAt: jwt.NewNumericDate(time.Now().Add(time.Hour)), ExpiresAt: jwt.NewNumericDate(time.Now().Add(2 * time.Hour))}}, jwt.SigningMethodHS256, jwtKey, jwt.ErrTokenUsedBeforeIssued},
		{"wrong key", Claims{RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}}, jwt.SigningMethodHS256, []byte("other-secret"), jwt.ErrTokenSignatureInvalid},
		{"wrong algorithm", Claims{RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}}, jwt.SigningMethodHS384, jwtKey, jwt.ErrTokenSignatureInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token, err := jwt.NewWithClaims(tc.method, tc.claims).SignedString(tc.key)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ValidateToken(token); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestGenerateTokenRejectsNonpositiveDuration(t *testing.T) {
	for _, duration := range []time.Duration{0, -time.Second} {
		if _, err := GenerateTokenWithDuration("u", duration); err == nil {
			t.Fatal("accepted nonpositive duration")
		}
	}
}
