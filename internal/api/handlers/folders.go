package handlers

import (
	"database/sql"
	"log"
	"net/http"

	"github.com/gorilla/mux"
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
			folderParams = database.GetFolderContentsParams{
				ParentFolderID: sql.NullString{},
				FolderID:       folderID,
			}
		} else {
			folderParams = database.GetFolderContentsParams{
				ParentFolderID: sql.NullString{
					String: folderID,
					Valid:  true,
				},
				FolderID: folderID,
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
		vars := mux.Vars(r)
		folderID := vars["ID"]

		var folderParams database.GetFolderContentsParams
		folderParams = database.GetFolderContentsParams{
			ParentFolderID: sql.NullString{Valid: false},
			FolderID:       folderID,
		}

		torrents, err := queries.GetFolderContents(r.Context(), folderParams)
		log.Printf("calling get torrents  homepage with id: %s, rootFolderID: %s: torrentsfiles: %+v\n\n", folderID, rootFolderID, torrents)
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
