package upload

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log"
	"path/filepath"

	"github.com/plutack/go-gofile/api"
	database "github.com/plutack/seedrlike/internal/database/sqlc"
)

type folderID = string

func newNullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{
		String: s,
		Valid:  true,
	}
}

func createFolder(folderName string, parentFolderID string, uploadClient *api.Api, db *database.Queries, hash string, size int64) (string, error) {
	info, err := uploadClient.CreateFolder(parentFolderID, folderName)
	if err != nil {
		return "", err
	}
	var parentFolderIDValue sql.NullString
	if info.Data.ParentFolder == parentFolderID {
		parentFolderIDValue = newNullString("")
	} else {
		parentFolderIDValue = newNullString(info.Data.ParentFolder)
	}

	folderDetails := database.CreateFolderParams{
		ID:             info.Data.ID,
		Name:           info.Data.Name,
		Hash:           newNullString(hash),
		Size:           size,
		ParentFolderID: parentFolderIDValue,
	}

	log.Printf("Creating folder: %s | Parent: %v | Hash: %s | Size: %d\n",
		info.Data.Name, info.Data.ParentFolder, hash, size)
	if err := db.CreateFolder(context.Background(), folderDetails); err != nil {
		return "", err
	}
	return info.Data.ID, nil
}

func uploadFile(fullFilePath string, parentFolderID string, uploadClient *api.Api, db *database.Queries, server string) error {
	info, err := uploadClient.UploadFile(server, fullFilePath, parentFolderID)

	if err != nil {
		return err
	}
	fileDetails := database.CreateFileParams{
		ID:       info.Data.ID,
		Name:     info.Data.Name,
		FolderID: info.Data.ParentFolder,
		Size:     info.Data.Size,
		Mimetype: info.Data.Mimetype,
		Md5:      info.Data.MD5,
	}
	if err := db.CreateFile(context.Background(), fileDetails); err != nil {
		return err
	}

	return nil

}

func SendFolderToServer(folderPath string, uploadClient *api.Api, rootFolderID string, server string, hash string, db *database.Queries) error {
	dirSizes := make(map[string]int64)
	err := filepath.WalkDir(folderPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("failed to get file info: %w", err)
		}
		if !d.IsDir() {
			// Add file size to all parent directories
			currentPath := filepath.Dir(path)
			for currentPath >= folderPath {
				dirSizes[currentPath] += info.Size()
				currentPath = filepath.Dir(currentPath)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("error calculating directory sizes: %w", err)
	}
	err = filepath.WalkDir(folderPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			relativePath, err := filepath.Rel(folderPath, path)
			if err != nil {
				return fmt.Errorf("failed to get relative path: %w", err)
			}
			dirSize := dirSizes[path]
			log.Printf("Creating folder: %s (Size: %d bytes)\n", relativePath, dirSize)

			newFolderID, createErr := createFolder(d.Name(), rootFolderID, uploadClient, db, hash, dirSize)
			if createErr != nil {
				return fmt.Errorf("failed to create folder %s: %w", d.Name(), createErr)
			}

			rootFolderID = newFolderID
			hash = ""
		} else {
			log.Printf("Uploading file: %s\n", path)
			if err := uploadFile(path, rootFolderID, uploadClient, db, server); err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("error during folder upload: %w", err)
	}

	return nil
}
