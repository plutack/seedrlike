package upload

import (
	"archive/zip"
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"io"
	"io/fs"
	"log"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"strings"

	kpflate "github.com/klauspost/compress/flate"
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

// ProgressTrackingUploader carries the identity needed to report upload
// progress over the websocket.
type ProgressTrackingUploader struct {
	websocketMgr *ws.WebsocketManager
	torrentID    string // InfoHash of the torrent
	torrentName  string // Name of the torrent
}

// NewProgressTrackingUploader creates a new upload tracker.
func NewProgressTrackingUploader(wm *ws.WebsocketManager, id string, name string) *ProgressTrackingUploader {
	return &ProgressTrackingUploader{
		websocketMgr: wm,
		torrentID:    id,
		torrentName:  name,
	}
}

func returnPercentageCompleted(c, t int64) float64 {
	percentage := (float64(c) / float64(t)) * 100
	percentage = min(percentage, 100)
	sizeInMB := float64(t) / 1000000.0
	log.Printf("%.2f%% completed out of %.2f MB", percentage, sizeInMB)
	return math.Round(percentage*100) / 100
}

func formatSpeed(bytesPerSec float64) string {
	if bytesPerSec < 1024 {
		return fmt.Sprintf("%.2f B/s", bytesPerSec)
	}
	value := bytesPerSec / 1024
	if value < 1024 {
		return fmt.Sprintf("%.2f KB/s", value)
	}
	return fmt.Sprintf("%.2f MB/s", value/1024.0)
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	} else if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}

// etaFromRate estimates remaining time given a byte rate.
func etaFromRate(completed, total int64, bytesPerSec float64) string {
	if total <= 0 || bytesPerSec <= 0 {
		return "calculating..."
	}
	if completed >= total {
		return "complete"
	}
	seconds := float64(total-completed) / bytesPerSec
	return formatDuration(time.Duration(seconds) * time.Second)
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
	torrentName string,
	userID *string) error {

	tracker := NewProgressTrackingUploader(wm, hash, torrentName)

	tracker.websocketMgr.SendProgress(ws.TorrentUpdate{
		Type:     "torrent update",
		ID:       tracker.torrentID,
		Name:     tracker.torrentName,
		Status:   "uploading",
		Progress: 0,
		Speed:    "-",
		ETA:      "starting upload...",
		UserID:   userID,
	})
	var (
		lastUploadEmit  time.Time
		lastUploadBytes int64
		uploadSampled   bool
	)
	callbackfunc := func(uploadedByte, totalByte int64) {
		// Throttle to the shared broadcast cadence so uploads tick at the same
		// rate as downloads (but always emit the final byte).
		if time.Since(lastUploadEmit) < ws.ProgressBroadcastInterval && uploadedByte < totalByte {
			return
		}
		speed := "-"
		eta := "calculating..."
		if uploadSampled {
			if dt := time.Since(lastUploadEmit).Seconds(); dt > 0 {
				bps := float64(uploadedByte-lastUploadBytes) / dt
				speed = formatSpeed(bps)
				eta = etaFromRate(uploadedByte, totalByte, bps)
			}
		}
		lastUploadEmit = time.Now()
		lastUploadBytes = uploadedByte
		uploadSampled = true
		tracker.websocketMgr.SendProgress(ws.TorrentUpdate{
			Type:           "torrent update",
			ID:             tracker.torrentID,
			Name:           tracker.torrentName,
			Status:         "uploading",
			Progress:       returnPercentageCompleted(uploadedByte, totalByte),
			Speed:          speed,
			ETA:            eta,
			TotalSize:      totalByte,
			BytesCompleted: uploadedByte,
			UserID:         userID,
		})

	}
	return SendTorrentToServer(folderPath, uploadClient, rootFolderID, server, hash, db, callbackfunc, userID)
}

func createFolder(folderName string, rootFolderID string, parentFolderID string, uploadClient *api.Api, db *database.Queries, hash string, size int64, userID *string) (string, error) {
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

	userIDParams := sql.NullString{}
	if userID != nil {
		userIDParams = sql.NullString{String: *userID, Valid: true}
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
		UserID:         userIDParams,
	}
	log.Printf("Creating folder in DB: ID=%s, Name=%s, Parent=%v, Hash=%s, Size=%d, UserID=%v\n",
		info.Data.ID, folderName, parentFolderID, hash, size, userID)

	if err := db.CreateFolder(context.Background(), folderDetails); err != nil {
		return "", fmt.Errorf("database error: %w", err)
	}

	return info.Data.ID, nil
}

func uploadFile(fullFilePath string, parentFolderID string, rootFolderID string, uploadClient *api.Api, db *database.Queries, server string, updateCallback progressCallbackFunc, userID *string) error {
	info, err := uploadClient.UploadFile(server, fullFilePath, parentFolderID, updateCallback)

	if err != nil {
		return err
	}
	folderID := info.Data.ParentFolder
	if folderID == rootFolderID {
		folderID = RootFolderPlaceholder
	}

	userIDParams := sql.NullString{}
	if userID != nil {
		userIDParams = sql.NullString{String: *userID, Valid: true}
	}

	fileDetails := database.CreateFileParams{
		ID:       info.Data.ID,
		Name:     info.Data.Name,
		FolderID: folderID,
		Size:     info.Data.Size,
		Mimetype: info.Data.Mimetype,
		Md5:      info.Data.MD5,
		Server:   info.Data.Servers[0],
		UserID:   userIDParams,
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

// zipDeflateLevel favors speed over ratio: these archives exist to bundle a
// folder into one upload to gofile, not to maximize compression. BestSpeed (1)
// is dramatically faster than the stdlib default (~6) for marginally larger output.
const zipDeflateLevel = kpflate.BestSpeed

// flateWriterPool reuses klauspost flate writers across entries so we don't
// allocate (and free) a compressor per file in a folder with many files.
var flateWriterPool = sync.Pool{
	New: func() any {
		w, _ := kpflate.NewWriter(io.Discard, zipDeflateLevel)
		return w
	},
}

// pooledFlateWriter returns its underlying writer to the pool on Close.
type pooledFlateWriter struct {
	*kpflate.Writer
}

func (p pooledFlateWriter) Close() error {
	err := p.Writer.Close()
	flateWriterPool.Put(p.Writer)
	return err
}

// newFlateCompressor is registered as the zip.Deflate compressor. It swaps the
// stdlib deflater for klauspost/compress (faster at the same level) and pools writers.
func newFlateCompressor(w io.Writer) (io.WriteCloser, error) {
	fw := flateWriterPool.Get().(*kpflate.Writer)
	fw.Reset(w)
	return pooledFlateWriter{fw}, nil
}

func ZipFolder(source string, destination string, wm *ws.WebsocketManager, progressCallback progressCallbackFunc) (err error) {
	zipFile, err := os.Create(destination)
	if err != nil {
		return err
	}
	// Capture the file Close error only if nothing else already failed.
	defer func() {
		if cerr := zipFile.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	totalSize, err := getPathSize(source)
	if err != nil {
		return fmt.Errorf("failed to calculate folder size: %w", err)
	}

	// Buffer writes to disk (1 MiB) so the zip writer's many small writes and the
	// raw copies of Store'd entries don't each hit a syscall.
	bufw := bufio.NewWriterSize(zipFile, 1<<20)
	zipWriter := zip.NewWriter(bufw)
	zipWriter.RegisterCompressor(zip.Deflate, newFlateCompressor)

	var totalRead int64
	copyBuf := make([]byte, 1<<20) // reused across files

	walkErr := filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
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

		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".mp4", ".mkv", ".avi", ".mov", ".wmv", ".flv", ".webm",
			".mp3", ".wav", ".flac", ".aac", ".ogg",
			".jpg", ".jpeg", ".png", ".gif", ".bmp", ".webp",
			".zip", ".rar", ".7z", ".tar", ".gz", ".iso", ".dmg", ".pkg":
			header.Method = zip.Store
		default:
			header.Method = zip.Deflate
		}
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
		_, err = io.CopyBuffer(zipEntry, srcFileWithProgress, copyBuf)
		return err
	})
	if walkErr != nil {
		return walkErr
	}

	// Order matters: flush the central directory into the buffer, then the buffer
	// to disk. Surface these errors instead of swallowing them in a defer — a
	// failed flush means a truncated/corrupt zip we'd otherwise report as success.
	if err := zipWriter.Close(); err != nil {
		return fmt.Errorf("failed to finalize zip: %w", err)
	}
	if err := bufw.Flush(); err != nil {
		return fmt.Errorf("failed to flush zip to disk: %w", err)
	}
	return nil
}

// SendTorrentToServer uploads a torrent to the server with optional progress updates
func SendTorrentToServer(folderPath string, uploadClient *api.Api, rootFolderID string, server string, hash string, db *database.Queries, progressCallback progressCallbackFunc, userID *string) error {
	// if content is a single torrent file
	info, err := os.Stat(folderPath)
	if err != nil {
		return err
	}

	if !info.IsDir() {
		// If it's a single file, upload it directly
		log.Printf("Uploading single file: %s to root folder: %s\n", folderPath, rootFolderID)
		return uploadFile(folderPath, rootFolderID, rootFolderID, uploadClient, db, server, progressCallback, userID)
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
	totalUploadSize := dirSize
	var bytesUploadedSoFar int64 = 0

	log.Printf("Creating initial folder: %s under parent: %s (Size: %d bytes)\n",
		baseName, rootFolderID, dirSize)

	initialFolderID, err := createFolder(baseName, rootFolderID, rootFolderID, uploadClient, db, hash, dirSize, userID)
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

			newFolderID, createErr := createFolder(d.Name(), rootFolderID, parentID, uploadClient, db, "", dirSize, userID)
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

			fileInfo, err := d.Info()
			if err != nil {
				return fmt.Errorf("failed to get file info for %s: %w", path, err)
			}

			wrappedCallback := func(read, _ int64) {
				if progressCallback != nil {
					progressCallback(bytesUploadedSoFar+read, totalUploadSize)
				}
			}

			if err := uploadFile(path, parentID, rootFolderID, uploadClient, db, server, wrappedCallback, userID); err != nil {
				return err
			}
			bytesUploadedSoFar += fileInfo.Size()
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("error during folder upload: %w", err)
	}

	return nil
}
