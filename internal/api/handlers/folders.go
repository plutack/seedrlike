package handlers

import (
	"log"
	"net/http"

	"github.com/gorilla/mux"
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
