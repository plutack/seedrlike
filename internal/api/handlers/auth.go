package handlers

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/plutack/seedrlike/internal/auth"
	database "github.com/plutack/seedrlike/internal/database/sqlc"
	"github.com/plutack/seedrlike/views/components"
	"github.com/plutack/seedrlike/views/layouts"
)

type AuthHandler struct {
	queries *database.Queries
}

func NewAuthHandler(q *database.Queries) *AuthHandler {
	return &AuthHandler{
		queries: q,
	}
}

func (a *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	if username == "" || password == "" {
		components.AuthError("Username and password required").Render(r.Context(), w)
		return
	}

	// Check if user exists
	_, err := a.queries.GetUserByUsername(r.Context(), username)
	if err == nil {
		components.AuthError("User already exists").Render(r.Context(), w)
		return
	} else if err != sql.ErrNoRows {
		log.Printf("Register check user error: %v", err)
		components.AuthError("Internal server error").Render(r.Context(), w)
		return
	}

	hashedPassword, err := auth.HashPassword(password)
	if err != nil {
		components.AuthError("Error creating user").Render(r.Context(), w)
		return
	}

	userID := uuid.New().String()
	err = a.queries.CreateUser(r.Context(), database.CreateUserParams{
		ID:           userID,
		Username:     username,
		PasswordHash: hashedPassword,
	})

	if err != nil {
		components.AuthError("Error creating user").Render(r.Context(), w)
		return
	}

	// Auto login
	token, err := auth.GenerateToken(userID)
	if err != nil {
		components.AuthError("Error logging in").Render(r.Context(), w)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    token,
		Expires:  time.Now().Add(24 * time.Hour),
		HttpOnly: true,
		Path:     "/",
	})

	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusOK)
}

func (a *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		components.AuthError("Invalid form data").Render(r.Context(), w)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	user, err := a.queries.GetUserByUsername(r.Context(), username)
	if err != nil {
		if err == sql.ErrNoRows {
			components.AuthError("Invalid credentials").Render(r.Context(), w)
		} else {
			log.Printf("Login error: %v", err)
			components.AuthError("Internal server error").Render(r.Context(), w)
		}
		return
	}

	if !auth.CheckPasswordHash(password, user.PasswordHash) {
		components.AuthError("Invalid credentials").Render(r.Context(), w)
		return
	}

	token, err := auth.GenerateToken(user.ID)
	if err != nil {
		components.AuthError("Error logging in").Render(r.Context(), w)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    token,
		Expires:  time.Now().Add(24 * time.Hour),
		HttpOnly: true,
		Path:     "/",
	})

	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusOK)
}

func (a *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    "",
		Expires:  time.Now().Add(-1 * time.Hour),
		HttpOnly: true,
		Path:     "/",
	})

	w.Header().Set("HX-Redirect", "/")
	w.WriteHeader(http.StatusOK)
}

func (a *AuthHandler) LoginPage(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") == "true" {
		components.AuthModal(true).Render(r.Context(), w)
		return
	}
	layouts.AuthLayout(components.AuthModal(true)).Render(r.Context(), w)
}

func (a *AuthHandler) RegisterPage(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") == "true" {
		components.AuthModal(false).Render(r.Context(), w)
		return
	}
	layouts.AuthLayout(components.AuthModal(false)).Render(r.Context(), w)
}

type contextKey string

const UserIDKey contextKey = "userID"

func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("token")
		if err != nil {
			if err == http.ErrNoCookie {
				// No token, proceed without user in context (guest)
				next.ServeHTTP(w, r)
				return
			}
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		claims, err := auth.ValidateToken(cookie.Value)
		if err != nil {
			// Invalid token, proceed as guest (or ask to re-login?)
			// For now, proceed as guest.
			next.ServeHTTP(w, r)
			return
		}

		ctx := context.WithValue(r.Context(), UserIDKey, claims.UserID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
