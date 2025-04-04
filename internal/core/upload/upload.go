package upload

import (
	"archive/zip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"github.com/plutack/go-gofile/api"
	database "github.com/plutack/seedrlike/internal/database/sqlc"
)

const RootFolderPlaceholder = "00000000-0000-0000-0000-000000000000"

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

func createFolder(folderName string, rootFolderID string, parentFolderID string, uploadClient *api.Api, db *database.Queries, hash string, size int64) (string, error) {
	// Skip parent folder check if this is a torrent root folder (it will have a hash)
	if parentFolderID != "" && hash == "" {
		// Only check parent existence for subfolders (non-root folders)
		exists, err := db.FolderExists(context.Background(), parentFolderID)
		if err != nil {
			return "", fmt.Errorf("failed to check parent folder existence: %w", err)
		}
		if !exists {
			return "", fmt.Errorf("parent folder %s does not exist in database", parentFolderID)
		}
	}

	// Create folder in storage service
	info, err := uploadClient.CreateFolder(parentFolderID, folderName)
	uploadClient.UpdateContent(info.Data.ID, "public", true)
	if err != nil {
		return "", fmt.Errorf("API error creating folder: %w", err)
	}

	// For the database entry:
	// - Otherwise, use the provided parent ID

	parentID := parentFolderID
	fmt.Printf("calling inside create folder: this is the the parent folder ID: %s ", parentFolderID)
	if parentFolderID == rootFolderID && hash != "" {
		parentID = RootFolderPlaceholder
	}

	folderDetails := database.CreateFolderParams{
		ID:   info.Data.ID,
		Name: folderName,
		Hash: sql.NullString{
			String: hash,
			Valid:  hash != "",
		},
		Size:           size,
		ParentFolderID: parentID,
	}
	log.Printf("Creating folder in DB: ID=%s, Name=%s, Parent=%v, Hash=%s, Size=%d\n",
		info.Data.ID, folderName, parentFolderID, hash, size)

	if err := db.CreateFolder(context.Background(), folderDetails); err != nil {
		return "", fmt.Errorf("database error: %w", err)
	}

	return info.Data.ID, nil
}

func uploadFile(fullFilePath string, parentFolderID string, rootFolderID string, uploadClient *api.Api, db *database.Queries, server string) error {
	info, err := uploadClient.UploadFile(server, fullFilePath, parentFolderID)

	if err != nil {
		return err
	}
	folderID := info.Data.ParentFolder
	if folderID == rootFolderID {
		folderID = RootFolderPlaceholder
	}

	fileDetails := database.CreateFileParams{
		ID:       info.Data.ID,
		Name:     info.Data.Name,
		FolderID: folderID,
		Size:     info.Data.Size,
		Mimetype: info.Data.Mimetype,
		Md5:      info.Data.MD5,
		Server:   info.Data.Servers[0],
	}
	if err := db.CreateFile(context.Background(), fileDetails); err != nil {
		return err
	}

	uploadClient.UpdateContent(info.Data.ParentFolder, "public", true)
	return nil

}

func ZipFolder(source string, destination string) error {

	zipFile, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(filepath.Dir(source), path)
		if err != nil {
			return err
		}

		// Use CreateHeader to enable ZIP64 support for large files
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = relPath
		header.Method = zip.Deflate // Enable compression
		if info.IsDir() {
			header.Name += "/"
		}

		zipEntry, err := zipWriter.CreateHeader(header)
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		_, err = io.Copy(zipEntry, srcFile)
		return err
	})
}

func SendTorrentToServer(folderPath string, uploadClient *api.Api, rootFolderID string, server string, hash string, db *database.Queries) error {
	// if content is a single torrent file
	info, err := os.Stat(folderPath)
	if err != nil {
		return err
	}

	if !info.IsDir() {
		// If it's a single file, upload it directly
		log.Printf("Uploading single file: %s to root folder: %s\n", folderPath, rootFolderID)
		return uploadFile(folderPath, rootFolderID, rootFolderID, uploadClient, db, server)
	}
	// Calculate directory sizes first
	dirSizes := make(map[string]int64)
	err = filepath.WalkDir(folderPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("failed to get file info: %w", err)
		}
		if !d.IsDir() {
			currentPath := filepath.Dir(path)
			for currentPath >= folderPath {
				dirSizes[currentPath] += info.Size()
				currentPath = filepath.Dir(currentPath)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Map to store folder IDs
	folderIDs := make(map[string]string)

	// Create the first folder under the provided root folder ID
	baseName := filepath.Base(folderPath)
	dirSize := dirSizes[folderPath]
	log.Printf("Creating initial folder: %s under parent: %s (Size: %d bytes)\n",
		baseName, rootFolderID, dirSize)

	initialFolderID, err := createFolder(baseName, rootFolderID, rootFolderID, uploadClient, db, hash, dirSize)
	if err != nil {
		return fmt.Errorf("failed to create initial folder: %w", err)
	}
	folderIDs[folderPath] = initialFolderID

	// Process all subfolders
	err = filepath.WalkDir(folderPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip the root directory as we've already created it
		if path == folderPath {
			return nil
		}

		if d.IsDir() {
			parentPath := filepath.Dir(path)
			parentID, exists := folderIDs[parentPath]
			if !exists {
				return fmt.Errorf("parent folder ID not found for: %s", path)
			}

			dirSize := dirSizes[path]
			log.Printf("Creating subfolder: %s under parent: %s (Size: %d bytes)\n",
				d.Name(), parentID, dirSize)

			newFolderID, createErr := createFolder(d.Name(), rootFolderID, parentID, uploadClient, db, "", dirSize)
			if createErr != nil {
				return fmt.Errorf("failed to create folder %s: %w", d.Name(), createErr)
			}
			folderIDs[path] = newFolderID
		} else {
			parentPath := filepath.Dir(path)
			parentID, exists := folderIDs[parentPath]
			if !exists {
				return fmt.Errorf("parent folder ID not found for file: %s", path)
			}

			log.Printf("Uploading file: %s to folder: %s\n", path, parentID)
			if err := uploadFile(path, parentID, rootFolderID, uploadClient, db, server); err != nil {
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
