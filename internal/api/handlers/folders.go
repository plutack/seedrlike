package handlers

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/plutack/go-gofile/api"
	"github.com/plutack/go-gofile/model"
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
		options, err := components.ParseLibraryOptions(r.URL.Query())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		r = r.WithContext(components.WithLibraryOptions(r.Context(), options))
		if options.Query == "" && folderID != rootFolderID && folderID != upload.RootFolderPlaceholder {
			folder, folderErr := queries.GetFolderByID(r.Context(), folderID)
			if folderErr == sql.ErrNoRows || (folderErr == nil && folder.UserID.Valid && folder.UserID.String != userID) {
				http.Error(w, "Folder not found", http.StatusNotFound)
				return
			}
			if folderErr != nil {
				http.Error(w, "Failed to fetch folder", http.StatusInternalServerError)
				return
			}
		}
		torrents, err := queries.GetLibraryContents(r.Context(), database.LibraryParams{
			ParentFolderID: parentFolderID, FolderID: parentFolderID, UserID: userIDParam,
			Query: options.Query, Sort: options.Sort, Order: options.Order,
		})

		slog.Debug("Fetching library contents", "operation", "list_library")

		// htmx navigations want only the list fragment; a direct browser visit
		// needs the full page (head/CSS/header) or it renders unstyled.
		// Pass the current folderID so the list's refresh targets THIS folder.
		isHTMX := r.Header.Get("HX-Request") == "true"

		if err != nil {
			slog.Error("Library request failed", "operation", "list_library", "reason", "database_query_failed", "error_type", fmt.Sprintf("%T", err))
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

		folderID := upload.RootFolderPlaceholder
		options, err := components.ParseLibraryOptions(r.URL.Query())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		r = r.WithContext(components.WithLibraryOptions(r.Context(), options))
		torrents, err := queries.GetLibraryContents(r.Context(), database.LibraryParams{
			ParentFolderID: folderID, FolderID: folderID, UserID: userIDParam,
			Query: options.Query, Sort: options.Sort, Order: options.Order,
		})
		if err != nil {
			slog.Error("Library request failed", "operation", "list_library", "reason", "database_query_failed", "error_type", fmt.Sprintf("%T", err))
			w.WriteHeader(http.StatusInternalServerError)
			if r.Header.Get("HX-Request") == "true" {
				components.DownloadList(true, nil, rootFolderID, userID != "").Render(r.Context(), w)
			} else {
				layouts.Base(true, nil, rootFolderID, userID != "").Render(r.Context(), w)
			}
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
		if userID == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if contentType != "file" && contentType != "folder" {
			http.Error(w, "Invalid content type", http.StatusBadRequest)
			return
		}
		if contentID == upload.RootFolderPlaceholder {
			http.Error(w, "Cannot delete the library root", http.StatusForbidden)
			return
		}
		user, err := queries.GetUserByID(r.Context(), userID)
		if err != nil {
			http.Error(w, "Failed to resolve user", http.StatusInternalServerError)
			return
		}
		owner, err := queries.GetLibraryContentOwner(r.Context(), contentID, contentType)
		if err == sql.ErrNoRows {
			http.Error(w, "Content not found", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, "Failed to resolve content owner", http.StatusInternalServerError)
			return
		}
		if !auth.IsAdmin(user.Username) {
			if !owner.Valid || owner.String != userID {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			if contentType == "folder" {
				owned, err := queries.LibrarySubtreeOwned(r.Context(), contentID, userID)
				if err != nil {
					http.Error(w, "Failed to verify folder ownership", http.StatusInternalServerError)
					return
				}
				if !owned {
					http.Error(w, "Folder contains content owned by another user", http.StatusForbidden)
					return
				}
			}
		}
		// Authorization must finish before any destructive remote request.
		// Delete from Gofile first
		err = deleteRemoteContentConfirmed(gofileClient, []string{contentID})
		if err != nil {
			slog.Error("Content deletion failed", "operation", "delete_content", "reason", "remote_delete_failed", "error_type", fmt.Sprintf("%T", err))
			http.Error(w, "Failed to delete from Gofile", http.StatusInternalServerError)
			return
		}
		slog.Info("Remote content deleted", "operation", "delete_content")

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
			slog.Error("Content deletion failed", "operation", "delete_content", "reason", "transaction_commit_failed", "error_type", fmt.Sprintf("%T", err))
			http.Error(w, "Failed to commit transaction", http.StatusInternalServerError)
			return
		}

		renderLibraryAfterDelete(w, r, queries)
	}
}

type contentDeleter interface {
	DeleteContent(...string) (model.DeleteContentResponse, error)
}

// The SDK does not turn API-level or per-item failures into Go errors. Require
// explicit confirmation for every item before removing local references.
func deleteRemoteContentConfirmed(client contentDeleter, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	response, err := client.DeleteContent(ids...)
	if err != nil {
		return fmt.Errorf("remote delete request failed: %w", err)
	}
	if response.Status != "ok" {
		return fmt.Errorf("remote delete not confirmed")
	}
	for _, id := range ids {
		item, ok := response.Data[id]
		if !ok || item.Status != "ok" {
			return fmt.Errorf("remote item deletion not confirmed")
		}
	}
	return nil
}

func DeleteStaleContentFromDB(queries *database.Queries, gofileClient contentDeleter, db *sql.DB) http.HandlerFunc {
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

		// A stale owned folder can contain another user's content. Never pass such
		// a subtree to the remote recursive deletion API.
		if !isAdmin {
			safeFolders := make([]string, 0, len(staleFolders))
			for _, id := range staleFolders {
				owned, err := queries.LibrarySubtreeOwned(r.Context(), id, userID)
				if err != nil {
					http.Error(w, "Failed to verify folder ownership", http.StatusInternalServerError)
					return
				}
				if owned && id != upload.RootFolderPlaceholder {
					safeFolders = append(safeFolders, id)
				}
			}
			staleFolders = safeFolders
		}
		// Fail closed before opening the DB transaction. A batch or transport
		// failure can have partial remote effects: retain ALL references rather
		// than guessing which files (including folder descendants) were removed.
		if err = deleteRemoteContentConfirmed(gofileClient, staleFiles); err != nil {
			slog.Error("Stale content deletion failed", "operation", "delete_stale_files", "reason", "remote_delete_unconfirmed", "error_type", fmt.Sprintf("%T", err))
			http.Error(w, "Gofile deletion could not be confirmed. Library records were preserved; some remote items may already have been removed.", http.StatusBadGateway)
			return
		}
		if err = deleteRemoteContentConfirmed(gofileClient, staleFolders); err != nil {
			slog.Error("Stale content deletion failed", "operation", "delete_stale_folders", "reason", "remote_delete_unconfirmed", "error_type", fmt.Sprintf("%T", err))
			http.Error(w, "Gofile deletion could not be confirmed. Library records were preserved; some remote items may already have been removed.", http.StatusBadGateway)
			return
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

		renderLibraryAfterDelete(w, r, queries)
	}
}
