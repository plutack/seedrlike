package auth

import (
	"errors"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

var jwtKey = []byte(getEnv("JWT_SECRET", "your_secret_key"))

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

type Claims struct {
	UserID string `json:"user_id"`
	jwt.RegisteredClaims
}

// IsAdmin reports whether the given username is the configured administrator.
// The admin account is designated via the ADMIN_USERNAME environment variable.
func IsAdmin(username string) bool {
	adminUsername := getEnv("ADMIN_USERNAME", "")
	return adminUsername != "" && username == adminUsername
}

func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	return string(bytes), err
}

func CheckPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

const (
	SessionTokenDuration  = 24 * time.Hour
	RememberTokenDuration = 30 * 24 * time.Hour
)

// GenerateToken preserves the default, non-renewing 24-hour token lifetime.
func GenerateToken(userID string) (string, error) {
	return GenerateTokenWithDuration(userID, SessionTokenDuration)
}

// GenerateTokenWithDuration issues a fixed-lifetime token. Validating a token
// never extends its expiry. JWT_SECRET must remain stable across server restarts.
func GenerateTokenWithDuration(userID string, duration time.Duration) (string, error) {
	if duration <= 0 {
		return "", errors.New("token duration must be positive")
	}
	now := time.Now()
	expirationTime := now.Add(duration)
	claims := &Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expirationTime),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtKey)
}

func ValidateToken(tokenString string) (*Claims, error) {
	claims := &Claims{}

	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		return jwtKey, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired(), jwt.WithIssuedAt())

	if err != nil {
		return nil, err
	}

	if !token.Valid {
		return nil, errors.New("invalid token")
	}

	return claims, nil
}
