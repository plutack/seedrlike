package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/plutack/seedrlike/internal/core/upload"
)

func TestDeleteRejectsBeforeRemoteCall(t *testing.T) {
	// Nil dependencies make these tests fail immediately if validation reaches
	// either the database or the destructive Gofile call.
	handler := DeleteContentFromDB(nil, nil, nil)
	for _, tc := range []struct {
		name, user, id, kind string
		status               int
	}{
		{"guest", "", "file", "file", http.StatusUnauthorized},
		{"invalid type", "user", "file", "invalid", http.StatusBadRequest},
		{"root protected", "user", upload.RootFolderPlaceholder, "folder", http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodDelete, "/contents/"+tc.id+"?type="+tc.kind, nil)
			r = r.WithContext(context.WithValue(r.Context(), UserIDKey, tc.user))
			r = mux.SetURLVars(r, map[string]string{"ID": tc.id})
			w := httptest.NewRecorder()
			handler(w, r)
			if w.Code != tc.status {
				t.Fatalf("got %d want %d", w.Code, tc.status)
			}
		})
	}
}

func TestLibraryRejectsOversizedSearchBeforeDatabase(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/?q="+string(make([]byte, 0)), nil)
	v := r.URL.Query()
	query := ""
	for i := 0; i < 201; i++ {
		query += "x"
	}
	v.Set("q", query)
	r.URL.RawQuery = v.Encode()
	w := httptest.NewRecorder()
	GetTorrentsFromDBHomepage(nil, "root")(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d", w.Code)
	}
}
