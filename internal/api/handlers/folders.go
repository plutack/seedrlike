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
		folderID := vars["folderID"]
		log.Println(rootFolderID)
		log.Printf("folder id from url: %s", folderID)

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
		if err != nil {
			log.Printf("Error fetching folder contents: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			components.DownloadList(false, nil, rootFolderID).Render(r.Context(), w)
			return
		}

		log.Println(torrents)
		w.WriteHeader(http.StatusOK)
		components.DownloadList(false, torrents, rootFolderID).Render(r.Context(), w)
	}
}

func GetTorrentsFromDBHomepage(queries *database.Queries, rootFolderID string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		folderID := vars["folderID"]
		log.Printf("folder id from url: %s", folderID)
		log.Println(rootFolderID)
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
		if err != nil {
			log.Printf("Error fetching folder contents: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			layouts.Base(false, nil, rootFolderID).Render(r.Context(), w)
			return
		}

		log.Println(torrents)
		w.WriteHeader(http.StatusOK)
		layouts.Base(false, torrents, rootFolderID).Render(r.Context(), w)
	}
}
