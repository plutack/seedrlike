package handlers

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/plutack/go-gofile/api"
	"github.com/plutack/seedrlike/internal/core/upload"
	database "github.com/plutack/seedrlike/internal/database/sqlc"
	"github.com/plutack/seedrlike/views/components"
	"github.com/plutack/seedrlike/views/layouts"
)

func GetTorrentsFromDB(queries *database.Queries, rootFolderID string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		folderID := vars["ID"]

		var folderParams database.GetFolderContentsParams
		if folderID == rootFolderID {
			rootFolderID = upload.RootFolderPlaceholder
			folderParams = database.GetFolderContentsParams{
				ParentFolderID: rootFolderID,
				FolderID:       folderID,
			}
		} else {
			folderParams = database.GetFolderContentsParams{
				ParentFolderID: folderID,
				FolderID:       folderID,
			}
		}
		torrents, err := queries.GetFolderContents(r.Context(), folderParams)

		log.Printf("calling get torrents with id: %s, rootFolderID: %s\n\n", folderID, rootFolderID)
		if err != nil {
			log.Printf("Error fetching folder contents: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			components.DownloadList(true, nil, rootFolderID).Render(r.Context(), w)
			return
		}

		w.WriteHeader(http.StatusOK)
		components.DownloadList(false, torrents, rootFolderID).Render(r.Context(), w)
	}
}

func GetTorrentsFromDBHomepage(queries *database.Queries, rootFolderID string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		var folderParams database.GetFolderContentsParams
		rootFolderID = upload.RootFolderPlaceholder
		folderParams = database.GetFolderContentsParams{
			ParentFolderID: rootFolderID,
			FolderID:       rootFolderID,
		}
		torrents, err := queries.GetFolderContents(r.Context(), folderParams)
		if err != nil {
			log.Printf("Error fetching folder contents: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			layouts.Base(true, nil, rootFolderID).Render(r.Context(), w)
			return
		}

		if r.Header.Get("HX-Request") == "true" {
			components.DownloadList(false, torrents, rootFolderID).Render(r.Context(), w)
			return
		}

		// Otherwise render the full page
		layouts.Base(false, torrents, rootFolderID).Render(r.Context(), w)
	}
}

func DeleteContentFromDB(queries *database.Queries, gofileClient *api.Api, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
			fmt.Println("error deleting 4: %s\n", err)
			http.Error(w, "Failed to commit transaction", http.StatusInternalServerError)
			return
		}

		// Fetch updated torrent list
		rootFolderID := upload.RootFolderPlaceholder
		folderParams := database.GetFolderContentsParams{
			ParentFolderID: rootFolderID,
			FolderID:       rootFolderID,
		}
		torrents, err := queries.GetFolderContents(r.Context(), folderParams)
		if err != nil {
			http.Error(w, "Failed to fetch updated list", http.StatusInternalServerError)
			return
		}

		// Render updated download list
		w.WriteHeader(http.StatusOK)
		components.DownloadList(false, torrents, rootFolderID).Render(r.Context(), w)
	}
}

func DeleteStaleContentFromDB(queries *database.Queries, gofileClient *api.Api, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Fetch stale files and folders before deletion
		staleFiles, err := queries.GetOldFiles(r.Context())
		if err != nil {
			http.Error(w, "Failed to fetch stale files", http.StatusInternalServerError)
			return
		}
		staleFolders, err := queries.GetOldFolders(r.Context())
		if err != nil {
			http.Error(w, "Failed to fetch stale folders", http.StatusInternalServerError)
			return
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

		for _, folderID := range staleFolders {
			err = qtx.DeleteFilesByFolderIDs(r.Context(), folderID)
			if err != nil {
				http.Error(w, fmt.Sprintf("Failed to delete files in folder %s: %v", folderID, err), http.StatusInternalServerError)
				return
			}
		}

		//  delete stale files
		for _, fileID := range staleFiles {
			err = qtx.DeleteFileByID(r.Context(), fileID)
			if err != nil {
				http.Error(w, fmt.Sprintf("Failed to delete file %s: %v", fileID, err), http.StatusInternalServerError)
				return
			}
		}

		//  delete the folders
		for _, folderID := range staleFolders {
			// Skip the root folder placeholder
			if folderID == upload.RootFolderPlaceholder {
				continue
			}

			err = qtx.DeleteFolderByID(r.Context(), folderID)
			if err != nil {
				http.Error(w, fmt.Sprintf("Failed to delete folder %s: %v", folderID, err), http.StatusInternalServerError)
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
		}
		torrents, err := queries.GetFolderContents(r.Context(), folderParams)
		if err != nil {
			http.Error(w, "Failed to fetch updated list", http.StatusInternalServerError)
			return
		}

		// Render updated download list
		w.WriteHeader(http.StatusOK)
		components.DownloadList(false, torrents, rootFolderID).Render(r.Context(), w)
	}
}
