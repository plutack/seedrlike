package handlers

import (
	"database/sql"
	"net/http"

	"github.com/plutack/seedrlike/internal/core/upload"
	database "github.com/plutack/seedrlike/internal/database/sqlc"
	"github.com/plutack/seedrlike/views/components"
)

func renderLibraryAfterDelete(w http.ResponseWriter, r *http.Request, queries *database.Queries) {
	options, err := components.ParseLibraryOptions(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	userID, _ := r.Context().Value(UserIDKey).(string)
	folderID := r.URL.Query().Get("folder")
	if folderID == "" {
		folderID = upload.RootFolderPlaceholder
	}
	if folderID != upload.RootFolderPlaceholder {
		folder, err := queries.GetFolderByID(r.Context(), folderID)
		if err == sql.ErrNoRows || (err == nil && folder.UserID.Valid && folder.UserID.String != userID) {
			http.Error(w, "Folder not found", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, "Failed to resolve folder", http.StatusInternalServerError)
			return
		}
	}
	ctx := components.WithLibraryOptions(r.Context(), options)
	torrents, err := queries.GetLibraryContents(ctx, database.LibraryParams{
		ParentFolderID: folderID, FolderID: folderID, UserID: sql.NullString{String: userID, Valid: userID != ""},
		Query: options.Query, Sort: options.Sort, Order: options.Order,
	})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
	components.DownloadList(err != nil, torrents, folderID, userID != "").Render(ctx, w)
}
