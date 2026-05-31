package handlers

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/plutack/go-gofile/api"
	"github.com/plutack/seedrlike/internal/auth"
	"github.com/plutack/seedrlike/internal/core/upload"
	database "github.com/plutack/seedrlike/internal/database/sqlc"
	"github.com/plutack/seedrlike/views/components"
	"github.com/plutack/seedrlike/views/layouts"
)

func GetTorrentsFromDB(queries *database.Queries, rootFolderID string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		folderID := vars["ID"]

		userID, _ := r.Context().Value(UserIDKey).(string)
		var userIDParam sql.NullString
		if userID != "" {
			userIDParam = sql.NullString{String: userID, Valid: true}
		}

		// Root-level items are stored under the placeholder parent id. Use a local
		// var for the parent lookup so we never mutate the captured rootFolderID
		// (the handler is built once; mutating it would corrupt later requests).
		parentFolderID := folderID
		if folderID == rootFolderID {
			parentFolderID = upload.RootFolderPlaceholder
		}
		folderParams := database.GetFolderContentsParams{
			ParentFolderID: parentFolderID,
			FolderID:       folderID,
			UserID:         userIDParam,
		}
		torrents, err := queries.GetFolderContents(r.Context(), folderParams)

		log.Printf("calling get torrents with id: %s, parentFolderID: %s\n\n", folderID, parentFolderID)

		// htmx navigations want only the list fragment; a direct browser visit
		// needs the full page (head/CSS/header) or it renders unstyled.
		// Pass the current folderID so the list's refresh targets THIS folder.
		isHTMX := r.Header.Get("HX-Request") == "true"

		if err != nil {
			log.Printf("Error fetching folder contents: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			if isHTMX {
				components.DownloadList(true, nil, folderID, userID != "").Render(r.Context(), w)
			} else {
				layouts.Base(true, nil, folderID, userID != "").Render(r.Context(), w)
			}
			return
		}

		w.WriteHeader(http.StatusOK)
		if isHTMX {
			components.DownloadList(false, torrents, folderID, userID != "").Render(r.Context(), w)
			return
		}
		layouts.Base(false, torrents, folderID, userID != "").Render(r.Context(), w)
	}
}

func GetTorrentsFromDBHomepage(queries *database.Queries, rootFolderID string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, _ := r.Context().Value(UserIDKey).(string)
		var userIDParam sql.NullString
		if userID != "" {
			userIDParam = sql.NullString{String: userID, Valid: true}
		}

		var folderParams database.GetFolderContentsParams
		rootFolderID = upload.RootFolderPlaceholder
		folderParams = database.GetFolderContentsParams{
			ParentFolderID: rootFolderID,
			FolderID:       rootFolderID,
			UserID:         userIDParam,
		}
		torrents, err := queries.GetFolderContents(r.Context(), folderParams)
		if err != nil {
			log.Printf("Error fetching folder contents: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			layouts.Base(true, nil, rootFolderID, userID != "").Render(r.Context(), w)
			return
		}

		if r.Header.Get("HX-Request") == "true" {
			components.DownloadList(false, torrents, rootFolderID, userID != "").Render(r.Context(), w)
			return
		}

		// Otherwise render the full page
		layouts.Base(false, torrents, rootFolderID, userID != "").Render(r.Context(), w)
	}
}

func DeleteContentFromDB(queries *database.Queries, gofileClient *api.Api, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, _ := r.Context().Value(UserIDKey).(string)
		vars := mux.Vars(r)
		contentID := vars["ID"]
		q := r.URL.Query()
		contentType := q.Get("type")
		fmt.Println(contentID)
		// Delete from Gofile first
		result, err := gofileClient.DeleteContent(contentID)
		if err != nil {
			fmt.Printf("err: %s ", err)
			http.Error(w, "Failed to delete from Gofile", http.StatusInternalServerError)
			return
		}
		fmt.Print(result)

		// Start a transaction
		tx, err := db.BeginTx(r.Context(), nil)
		if err != nil {
			http.Error(w, "Failed to start transaction", http.StatusInternalServerError)
			return
		}
		defer tx.Rollback()

		qtx := queries.WithTx(tx)

		if contentType == "folder" {
			// Get all folders to delete
			folderIDs, err := qtx.GetFoldersToDelete(r.Context(), contentID)
			if err != nil {
				http.Error(w, "Failed to identify folders to delete", http.StatusInternalServerError)
				return
			}

			// Delete all files in those folders
			for _, folderID := range folderIDs {
				err = qtx.DeleteFilesByFolderIDs(r.Context(), folderID)
				if err != nil {
					http.Error(w, "Failed to delete files", http.StatusInternalServerError)
					return
				}
			}

			// Delete all folders (in reverse order to handle parent-child relationships)
			for i := len(folderIDs) - 1; i >= 0; i-- {
				err = qtx.DeleteFolderByID(r.Context(), folderIDs[i])
				if err != nil {
					http.Error(w, "Failed to delete folders", http.StatusInternalServerError)
					return
				}
			}
		} else if contentType == "file" {
			err = qtx.DeleteFileByID(r.Context(), contentID)
		} else {
			http.Error(w, "Invalid content type", http.StatusBadRequest)
			return
		}

		if err != nil {
			http.Error(w, "Database deletion failed", http.StatusInternalServerError)
			return
		}

		// Commit the transaction

		if err := tx.Commit(); err != nil {
			fmt.Printf("error deleting 4: %s\n", err)
			http.Error(w, "Failed to commit transaction", http.StatusInternalServerError)
			return
		}

		// Fetch updated torrent list
		rootFolderID := upload.RootFolderPlaceholder
		folderParams := database.GetFolderContentsParams{
			ParentFolderID: rootFolderID,
			FolderID:       rootFolderID,
			UserID:         sql.NullString{String: userID, Valid: userID != ""},
		}
		torrents, err := queries.GetFolderContents(r.Context(), folderParams)
		if err != nil {
			http.Error(w, "Failed to fetch updated list", http.StatusInternalServerError)
			return
		}

		// Render updated download list
		w.WriteHeader(http.StatusOK)
		components.DownloadList(false, torrents, rootFolderID, userID != "").Render(r.Context(), w)
	}
}

func DeleteStaleContentFromDB(queries *database.Queries, gofileClient *api.Api, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Only authenticated users may clear stale content.
		userID, _ := r.Context().Value(UserIDKey).(string)
		if userID == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Determine whether the logged-in user is the administrator.
		user, err := queries.GetUserByID(r.Context(), userID)
		if err != nil {
			http.Error(w, "Failed to resolve user", http.StatusInternalServerError)
			return
		}
		isAdmin := auth.IsAdmin(user.Username)

		// Fetch stale files and folders before deletion.
		// Admins wipe everything stale; regular users only clear their own content.
		var staleFiles, staleFolders []string
		if isAdmin {
			staleFiles, err = queries.GetOldFiles(r.Context())
			if err != nil {
				http.Error(w, "Failed to fetch stale files", http.StatusInternalServerError)
				return
			}
			staleFolders, err = queries.GetOldFolders(r.Context())
			if err != nil {
				http.Error(w, "Failed to fetch stale folders", http.StatusInternalServerError)
				return
			}
		} else {
			userIDParam := sql.NullString{String: userID, Valid: true}
			staleFiles, err = queries.GetOldFilesByUser(r.Context(), userIDParam)
			if err != nil {
				http.Error(w, "Failed to fetch stale files", http.StatusInternalServerError)
				return
			}
			staleFolders, err = queries.GetOldFoldersByUser(r.Context(), userIDParam)
			if err != nil {
				http.Error(w, "Failed to fetch stale folders", http.StatusInternalServerError)
				return
			}
		}

		// Delete from Gofile first
		if len(staleFiles) != 0 {
			_, err = gofileClient.DeleteContent(staleFiles...)
			if err != nil {
				fmt.Printf("Failed to delete stale files from Gofile: %v\n", err)
			}
		}
		if len(staleFolders) != 0 {
			_, err = gofileClient.DeleteContent(staleFolders...)
			if err != nil {
				fmt.Printf("Failed to delete stale folders from Gofile: %v\n", err)
			}
		}

		// Start a transaction
		tx, err := db.BeginTx(r.Context(), nil)
		if err != nil {
			http.Error(w, "Failed to start transaction", http.StatusInternalServerError)
			return
		}
		defer tx.Rollback()
		qtx := queries.WithTx(tx)

		// Delete each stale folder as a subtree, removing children before their
		// parents so folder->folder foreign keys are never violated. A folder may
		// appear inside another stale folder's subtree, so track what's been
		// handled to avoid deleting it twice.
		deleted := make(map[string]bool)
		for _, staleFolderID := range staleFolders {
			if deleted[staleFolderID] {
				continue
			}

			// Returns the folder and all its descendants in parent->child order.
			subtree, err := qtx.GetFoldersToDelete(r.Context(), staleFolderID)
			if err != nil {
				http.Error(w, fmt.Sprintf("Failed to identify subtree for folder %s: %v", staleFolderID, err), http.StatusInternalServerError)
				return
			}

			// Remove the files in every folder of the subtree.
			for _, folderID := range subtree {
				if err := qtx.DeleteFilesByFolderIDs(r.Context(), folderID); err != nil {
					http.Error(w, fmt.Sprintf("Failed to delete files in folder %s: %v", folderID, err), http.StatusInternalServerError)
					return
				}
			}

			// Remove the folders child-first (reverse of parent->child order).
			for i := len(subtree) - 1; i >= 0; i-- {
				folderID := subtree[i]
				if folderID == upload.RootFolderPlaceholder || deleted[folderID] {
					continue
				}
				if err := qtx.DeleteFolderByID(r.Context(), folderID); err != nil {
					http.Error(w, fmt.Sprintf("Failed to delete folder %s: %v", folderID, err), http.StatusInternalServerError)
					return
				}
				deleted[folderID] = true
			}
		}

		// Delete any remaining stale files (e.g. files sitting directly in the
		// root folder, not under a stale folder). Files removed above are
		// already gone, so deleting by ID here is a harmless no-op for them.
		for _, fileID := range staleFiles {
			if err := qtx.DeleteFileByID(r.Context(), fileID); err != nil {
				http.Error(w, fmt.Sprintf("Failed to delete file %s: %v", fileID, err), http.StatusInternalServerError)
				return
			}
		}

		// Commit the transaction
		if err := tx.Commit(); err != nil {
			http.Error(w, "Failed to commit transaction", http.StatusInternalServerError)
			return
		}

		// Fetch updated torrent list
		rootFolderID := upload.RootFolderPlaceholder
		folderParams := database.GetFolderContentsParams{
			ParentFolderID: rootFolderID,
			FolderID:       rootFolderID,
			UserID:         sql.NullString{String: userID, Valid: userID != ""},
		}
		torrents, err := queries.GetFolderContents(r.Context(), folderParams)
		if err != nil {
			http.Error(w, "Failed to fetch updated list", http.StatusInternalServerError)
			return
		}

		// Render updated download list (user is authenticated here)
		w.WriteHeader(http.StatusOK)
		components.DownloadList(false, torrents, rootFolderID, true).Render(r.Context(), w)
	}
}
