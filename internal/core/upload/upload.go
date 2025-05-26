package upload

import (
	"archive/zip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"io/fs"
	"log"
	"math"
	"os"
	"path/filepath"

	"github.com/plutack/go-gofile/api"
	ws "github.com/plutack/seedrlike/internal/core/websocket"
	database "github.com/plutack/seedrlike/internal/database/sqlc"
)

const RootFolderPlaceholder = "00000000-0000-0000-0000-000000000000"

type progressCallbackFunc func(byteRead, totalBytes int64)
type folderID = string

// readerProgress wraps an io.Reader to track read progress
type readerProgress struct {
	io.Reader
	totalRead *int64
	totalSize int64
	onRead    progressCallbackFunc
}

// Read implements io.Reader and updates progress
func (rp *readerProgress) Read(p []byte) (n int, err error) {
	n, err = rp.Reader.Read(p)
	if n > 0 {
		*rp.totalRead += int64(n)
		if rp.onRead != nil {
			rp.onRead(*rp.totalRead, rp.totalSize)
		}
	}
	return
}

func newNullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{
		String: s,
		Valid:  true,
	}
}

// Add this to the upload.go file to enable progress tracking during uploads

// ProgressTrackingUploader handles upload progress reporting
type ProgressTrackingUploader struct {
	client          *api.Api
	websocketMgr    *ws.WebsocketManager
	torrentID       string  // InfoHash of the torrent
	torrentName     string  // Name of the torrent
	totalBytes      int64   // Total bytes to upload
	totalUploaded   int64   // Running total of bytes uploaded
	progressPercent float64 // Current progress percentage
}

// NewProgressTrackingUploader creates a new upload tracker
func NewProgressTrackingUploader(client *api.Api, wm *ws.WebsocketManager, id string, name string, totalSize int64) *ProgressTrackingUploader {
	return &ProgressTrackingUploader{
		client:        client,
		websocketMgr:  wm,
		torrentID:     id,
		torrentName:   name,
		totalBytes:    totalSize,
		totalUploaded: 0,
	}
}

// UpdateProgress reports upload progress through the websocket
func (u *ProgressTrackingUploader) UpdateProgress(bytesUploaded int64) {
	u.totalUploaded += bytesUploaded
	if u.totalBytes > 0 {
		newProgress := float64(u.totalUploaded) * 100 / float64(u.totalBytes)
		// Only send update if progress changed significantly (every 1%)
		if int(newProgress) > int(u.progressPercent) {
			u.progressPercent = newProgress
			// Round to 2 decimal places
			roundedProgress := math.Round(newProgress*100) / 100

			u.websocketMgr.SendProgress(ws.TorrentUpdate{
				Type:     "torrent update",
				ID:       u.torrentID,
				Name:     u.torrentName,
				Status:   "uploading",
				Progress: roundedProgress,
				Speed:    "-",
				ETA:      fmt.Sprintf("%.1f%%", roundedProgress),
			})
		}
	}
}

func returnPercentageCompleted(c, t int64) float64 {
	percentage := (float64(c) / float64(t)) * 100
	percentage = min(percentage, 100)
	sizeInMB := float64(t) / 1000000.0
	log.Printf("%.2f%% completed out of %.2f MB", percentage, sizeInMB)
	return math.Round(percentage*100) / 100
}

// SendTorrentToServerWithProgress uploads a torrent to the server with progress tracking
func SendTorrentToServerWithProgress(
	folderPath string,
	uploadClient *api.Api,
	rootFolderID string,
	server string,
	hash string,
	db *database.Queries,
	wm *ws.WebsocketManager,
	torrentName string) error {

	// Get total size for progress reporting
	totalSize, err := getPathSize(folderPath)
	if err != nil {
		log.Printf("Failed to calculate size for progress tracking: %v", err)
	}

	tracker := NewProgressTrackingUploader(uploadClient, wm, hash, torrentName, totalSize)

	tracker.websocketMgr.SendProgress(ws.TorrentUpdate{
		Type:     "torrent update",
		ID:       tracker.torrentID,
		Name:     tracker.torrentName,
		Status:   "uploading",
		Progress: 0,
		Speed:    "-",
		ETA:      "starting upload...",
	})
	callbackfunc := func(uploadedByte, totalByte int64) {
		tracker.websocketMgr.SendProgress(ws.TorrentUpdate{
			Type:     "torrent update",
			ID:       tracker.torrentID,
			Name:     tracker.torrentName,
			Status:   "uploading",
			Progress: returnPercentageCompleted(uploadedByte, totalByte),
			Speed:    "-",
			ETA:      "starting upload...",
		})

	}
	return SendTorrentToServer(folderPath, uploadClient, rootFolderID, server, hash, db, callbackfunc)
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
	if err != nil {
		return "", fmt.Errorf("API error creating folder: %w", err)
	}

	// Make folder public
	uploadClient.UpdateContent(info.Data.ID, "public", true)

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

func uploadFile(fullFilePath string, parentFolderID string, rootFolderID string, uploadClient *api.Api, db *database.Queries, server string, updateCallback progressCallbackFunc) error {
	info, err := uploadClient.UploadFile(server, fullFilePath, parentFolderID, updateCallback)

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

func getPathSize(path string) (int64, error) {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return 0, err
	}

	if !fileInfo.IsDir() {
		// It's a file, return its size
		return fileInfo.Size(), nil
	}

	// It's a directory, walk through its contents
	var totalSize int64
	err = filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			totalSize += info.Size()
		}
		return nil
	})

	return totalSize, err
}

func ZipFolder(source string, destination string, wm *ws.WebsocketManager, progressCallback progressCallbackFunc) error {
	zipFile, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer zipFile.Close()

	totalSize, err := getPathSize(source)
	if err != nil {
		return fmt.Errorf("failed to calculate folder size: %w", err)
	}

	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	var totalRead int64

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

		srcFileWithProgress := &readerProgress{
			Reader:    srcFile,
			totalRead: &totalRead,
			totalSize: totalSize,
			onRead:    progressCallback,
		}
		_, err = io.Copy(zipEntry, srcFileWithProgress)
		return err
	})
}

// SendTorrentToServer uploads a torrent to the server with optional progress updates
func SendTorrentToServer(folderPath string, uploadClient *api.Api, rootFolderID string, server string, hash string, db *database.Queries, progressCallback progressCallbackFunc) error {
	// if content is a single torrent file
	info, err := os.Stat(folderPath)
	if err != nil {
		return err
	}

	if !info.IsDir() {
		// If it's a single file, upload it directly
		log.Printf("Uploading single file: %s to root folder: %s\n", folderPath, rootFolderID)
		return uploadFile(folderPath, rootFolderID, rootFolderID, uploadClient, db, server, progressCallback)
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
			if err := uploadFile(path, parentID, rootFolderID, uploadClient, db, server, progressCallback); err != nil {
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
