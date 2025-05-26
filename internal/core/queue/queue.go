package queue

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/plutack/go-gofile/api"
	"github.com/plutack/seedrlike/internal/core/upload"
	ws "github.com/plutack/seedrlike/internal/core/websocket"
	database "github.com/plutack/seedrlike/internal/database/sqlc"
)

const (
	StatusNextPhase     = "Starting next phase"
	StatusPending       = "pending"
	StatusStarted       = "started"
	StatusTaskCompleted = "completed"
	StatusUploading     = "uploading"
	StatusDownloading   = "downloading"
	StatusFailed        = "failed"
	StatusZipping       = "zipping"
	StatusStopped       = "stopped"
	maxQueueSize        = 10 // TODO: implement a configuaration file
)

type (
	magnetLink = string

	DownloadTask struct {
		ID      string
		Request DownloadRequest
		Torrent *torrent.Torrent
		Status  string
	}

	DownloadQueue struct {
		mu    sync.Mutex
		tasks []*DownloadTask
	}

	DownloadRequest struct {
		MagnetLink string
		IsZipped   bool
	}
)

var (
	errorQueueFull = errors.New("Download Queue full")
	storagePath    = "/home/plutack/Downloads/seedrlike"
)

func NewDownloadTask(r DownloadRequest) *DownloadTask {
	t := &DownloadTask{
		ID:      "",
		Request: r,
		Status:  StatusPending,
	}
	return t
}

func New() *DownloadQueue {
	return &DownloadQueue{
		tasks: make([]*DownloadTask, 0, maxQueueSize),
	}
}

func (q *DownloadQueue) Add(r DownloadRequest) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.tasks) >= maxQueueSize {
		log.Println("Queue full")
		return errors.New("Queue Full")
	}
	t := NewDownloadTask(r)
	q.tasks = append(q.tasks, t)
	log.Println("New task added successfully to queue")
	return nil
}

// Stop attempts to stop a download task by its ID (info hash).
func (q *DownloadQueue) Stop(taskID string) error {
	if taskID == "" {
		return errors.New("cannot stop task with empty ID")
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	var taskToStop *DownloadTask
	var taskIndex int = -1 // Keep track of index for potential removal

	// Find the task by ID
	for i, task := range q.tasks {
		if task.ID == taskID {
			taskToStop = task
			taskIndex = i
			break
		}
	}

	if taskToStop == nil {
		log.Printf("Stop request failed: Task ID %s not found in queue.", taskID)
		return fmt.Errorf("task with ID %s not found", taskID)
	}

	log.Printf("Stop requested for task %s (Current Status: %s)", taskID, taskToStop.Status)

	// Handle based on current status
	switch taskToStop.Status {
	case StatusPending:
		// Task hasn't started downloading yet, just remove it
		log.Printf("Removing pending task %s (ID: %s) from queue.", taskToStop.Request.MagnetLink, taskID)
		q.tasks = append(q.tasks[:taskIndex], q.tasks[taskIndex+1:]...) // Remove using index
		return nil                                                      // Successfully removed pending task

	case StatusStarted, StatusDownloading:
		// Task is actively downloading or starting
		if taskToStop.Torrent == nil {
			log.Printf("Error stopping task %s: Status is %s but Torrent handle is nil.", taskID, taskToStop.Status)
			taskToStop.Status = StatusFailed
			return errors.New("cannot stop task, internal state error (nil torrent handle)")
		}

		log.Printf("Stopping active torrent download for task %s", taskID)
		taskToStop.Status = StatusStopped // Signal ProcessTasks loop to break

		return nil

	case StatusStopped:
		log.Printf("Task %s is already stopping or stopped.", taskID)
		return fmt.Errorf("task %s already stopping/stopped", taskID)

	case StatusTaskCompleted, StatusFailed:
		log.Printf("Task %s is already completed or failed, cannot stop.", taskID)
		return fmt.Errorf("task %s already completed/failed", taskID)

	default:
		log.Printf("Cannot stop task %s with unknown status: %s", taskID, taskToStop.Status)
		return fmt.Errorf("cannot stop task with status %s", taskToStop.Status)
	}
}

// Helper function to remove task by ID (must be called within a mutex lock)
func (q *DownloadQueue) removeTaskByID_unsafe(id string) {
	if id == "" {
		log.Println("Warning: Attempted to remove task with empty ID")
		return // Cannot remove if ID was never set
	}
	newTasks := make([]*DownloadTask, 0, len(q.tasks))
	for _, task := range q.tasks {
		if task.ID != id {
			newTasks = append(newTasks, task)
		} else {
			log.Printf("Removing task %s from queue slice", id)
		}
	}
	q.tasks = newTasks
}

func getFolderPath(folderName string) string {
	return fmt.Sprintf("%s/%s", storagePath, folderName)
}
func ProcessTasks(c *torrent.Client, q *DownloadQueue, u *api.Api, r string, db *database.Queries, wm *ws.WebsocketManager) {
	log.Println("Task processor started")
	for {
		var taskToProcess *DownloadTask

		q.mu.Lock()
		for i, task := range q.tasks {
			if task.Status == StatusPending {
				taskToProcess = task
				task.Status = StatusStarted
				log.Printf("Download for Task at index: %d for magnet link: %s started", i, taskToProcess.Request.MagnetLink)
				break
			}
		}
		q.mu.Unlock()
		if taskToProcess == nil {
			time.Sleep(5 * time.Second)
			continue
		}

		t, err := c.AddMagnet(taskToProcess.Request.MagnetLink)
		if err != nil {
			log.Println("error adding link to client for download")
			q.mu.Lock()
			taskToProcess.Status = StatusFailed
			// remove here?
			q.mu.Unlock()
			continue
		}

		// Initial "pending" update
		wm.SendProgress(ws.TorrentUpdate{
			Type:     "torrent update",
			ID:       t.InfoHash().String(),
			Name:     "unknown",
			Status:   StatusPending,
			Progress: 0,
			Speed:    "0",
			ETA:      "calculating...",
		})
		log.Printf("Waiting for torrent info for magnet link: %s", taskToProcess.Request.MagnetLink)
		infoCtx, cancelInfo := context.WithTimeout(context.Background(), 1*time.Minute)
		select {
		case <-t.GotInfo():
			log.Printf("Got info successfully for %s:", t.Info().Name)
		case <-infoCtx.Done():
			// torrent is probably dead
			log.Printf("Torrent is no longer active")
			t.Drop()
			q.mu.Lock()
			taskToProcess.Status = StatusFailed
			// remove task here too
			wm.SendProgress(ws.TorrentUpdate{
				Type:     "torrent update",
				ID:       t.InfoHash().String(),
				Name:     "unknown",
				Status:   StatusFailed,
				Progress: 0,
				Speed:    "0",
				ETA:      "--:--",
			})
			q.mu.Unlock()
			cancelInfo()
			continue
		}
		cancelInfo()
		infoHash := t.InfoHash().String()
		q.mu.Lock()
		taskToProcess.ID = infoHash
		taskToProcess.Torrent = t
		log.Printf("Task %s (%s) updated with InfoHash ID and torrent object.", t.Info().Name, infoHash)
		q.mu.Unlock()

		// Start download
		t.DownloadAll()
		log.Printf("%s started downloading", t.Info().Name)
		t.DisallowDataUpload()

		// Channel to stop Goroutines once complete
		stopChan := make(chan struct{})

		// Start Goroutine for speed and ETA updates

		var wg sync.WaitGroup
		wg.Add(1)

		go func(currentTask *DownloadTask) {
			defer wg.Done()
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-stopChan:
					log.Printf("Stopping progress updates for %s", currentTask.ID)
					return
				case <-ticker.C:
					q.mu.Lock()
					currentStatus := currentTask.Status
					q.mu.Unlock()
					if currentStatus != StatusStarted && currentStatus != StatusDownloading {
						log.Printf("Task %s status changed to %s, stopping progress updates.", currentTask.ID, currentStatus)
						return // Exit if task status changed externally (e.g., stopped)
					}
					torrentHandle := currentTask.Torrent
					if torrentHandle.Info() == nil {
						log.Printf("Torrent info not yet available for progress update %s", currentTask.ID)
						continue
					}

					speed := getDownloadSpeed(torrentHandle, 1*time.Second) // Shorter duration for calculation?
					eta := calculateETA(torrentHandle)
					progress := returnPercentageCompleted(torrentHandle.BytesCompleted(), torrentHandle.Length())

					q.mu.Lock()
					// Only update to Downloading if it was Started
					if currentTask.Status == StatusStarted {
						currentTask.Status = StatusDownloading
					}
					q.mu.Unlock()
					wm.SendProgress(ws.TorrentUpdate{
						Type:     "torrent update",
						ID:       currentTask.ID,
						Name:     torrentHandle.Info().Name,
						Status:   StatusDownloading,
						Progress: progress,
						Speed:    speed,
						ETA:      eta,
					})
				}
			}
		}(taskToProcess)

		// Wait until torrent is complete
		for !t.Complete().Bool() {
			q.mu.Lock()
			currentStatus := taskToProcess.Status
			q.mu.Unlock()
			if currentStatus == StatusStopped {
				log.Printf("Download loop interrupted for %s because status is %s", infoHash, currentStatus)
				break
			}
			time.Sleep(1 * time.Second)
		}

		// Stop the update Goroutine
		log.Printf("Download loop finished/interrupted for %s. Closing stopChan.", infoHash)
		close(stopChan)
		wg.Wait()
		log.Printf("Progress update goroutine finished for %s.", infoHash)

		nextPhase := StatusNextPhase
		// Check if it was stopped externally (implement Stop method later)
		q.mu.Lock()
		if taskToProcess.Status == StatusStopped {
			nextPhase = StatusStopped
			log.Printf("Task %s marked as Stopped.", infoHash)
		} else if !t.Complete().Bool() {
			// It exited the loop but isn't complete and wasn't stopped -> Failed?
			nextPhase = StatusFailed
			log.Printf("Task %s exited download loop but is not complete and not stopped. Marking as Failed.", infoHash)
		} else {
			log.Printf("Task %s completed successfully.", infoHash)
		}
		taskToProcess.Status = nextPhase
		q.mu.Unlock()

		// Send final websocket update based on status
		wm.SendProgress(ws.TorrentUpdate{
			Type:     "torrent update",
			ID:       infoHash,
			Name:     t.Info().Name,
			Status:   nextPhase,                                                 // completed, failed, stopped
			Progress: returnPercentageCompleted(t.BytesCompleted(), t.Length()), // Use final progress
			Speed:    "0",
			ETA:      "--:--", // "completed", "failed", "stopped"
		})

		log.Printf("Dropping torrent client state for %s", infoHash)
		t.Drop() // Essential cleanup

		// Determine paths before checking status for upload
		originalPath := ""   // Initialize
		if t.Info() != nil { // Make sure info is available
			originalPath = getFolderPath(t.Info().Name)
		} else {
			log.Printf("Warning: Cannot determine originalPath for cleanup as torrent info is nil for task %s", infoHash)
		}
		uploadPath := originalPath

		//  Upload and Cleanup (only if completed successfully or stopped)
		if nextPhase == StatusNextPhase {
			log.Printf("Starting upload/cleanup for completed task %s", infoHash)
			wm.SendProgress(ws.TorrentUpdate{
				Type:     "torrent update",
				ID:       infoHash,
				Name:     t.Info().Name,
				Status:   StatusUploading,
				Progress: 0.00,
				Speed:    "0",
				ETA:      "uploading...",
			})
			availableServerInfo, err := u.GetAvailableServers("eu")
			if err != nil {
				log.Printf("Error getting Gofile server for %s: %v. Skipping upload.", infoHash, err)
				// Update status to Failed? Or add a new "UploadFailed" status? not likely to happen though
				q.mu.Lock()
				taskToProcess.Status = StatusFailed // Mark as failed if server fetch fails
				q.mu.Unlock()
				// TODO: cleanup? and continue to eliminate nested else/ifs
			} else {
				euServer := availableServerInfo.Data.Servers[0].Name // TODO: create a function to randomize server selection
				fmt.Printf("selected server:%s", euServer)
				uploadPath = originalPath

				if taskToProcess.Request.IsZipped {
					zipPath := originalPath + ".zip"
					log.Printf("Zipping folder %s to %s", originalPath, zipPath)
					calculateZipProgress := func(readByte, totalByte int64) {
						var progress float64 = 0
						if totalByte > 0 {
							progress = float64(readByte) * 100 / float64(totalByte)
						}

						// Round to 2 decimal places
						progress = math.Round(progress*100) / 100

						wm.SendProgress(ws.TorrentUpdate{
							Type:     "torrent update",
							ID:       infoHash,
							Name:     t.Info().Name,
							Status:   StatusZipping,
							Progress: progress,
							Speed:    "-",
							ETA:      "--:--",
						})
					}
					if err = upload.ZipFolder(originalPath, zipPath, wm, calculateZipProgress); err != nil {
						log.Printf("Error creating zip for %s: %v", infoHash, err)
						q.mu.Lock()
						taskToProcess.Status = StatusFailed // Mark as failed if zip fails
						q.mu.Unlock()
						nextPhase = StatusFailed
						wm.SendProgress(ws.TorrentUpdate{
							Type:     "torrent update",
							ID:       infoHash,
							Name:     t.Info().Name,
							Status:   nextPhase,
							Progress: 0,
							Speed:    "-",
							ETA:      "--:--",
						})
					} else {
						uploadPath = zipPath
						wm.SendProgress(ws.TorrentUpdate{
							Type:     "torrent update",
							ID:       infoHash,
							Name:     t.Info().Name,
							Status:   StatusNextPhase,
							Progress: 0,
							Speed:    "-",
							ETA:      "--:--",
						})
					}
				}
				// Proceed with upload only if zip didn't fail (or wasn't requested)
				if nextPhase == StatusNextPhase {
					log.Printf("Uploading %s to Gofile server %s", uploadPath, euServer)
					wm.SendProgress(ws.TorrentUpdate{
						Type:     "torrent update",
						ID:       infoHash,
						Name:     t.Info().Name,
						Status:   StatusUploading,
						Progress: 0,
						Speed:    "-",
						ETA:      "uploading...",
					})
					err = upload.SendTorrentToServerWithProgress(uploadPath, u, r, euServer, infoHash, db, wm, t.Info().Name)
					if err != nil {
						log.Printf("Failed to upload %s to gofile for %s: %s", uploadPath, infoHash, err)
						nextPhase = StatusFailed
						q.mu.Lock()
						taskToProcess.Status = nextPhase
						q.mu.Unlock()
						wm.SendProgress(ws.TorrentUpdate{
							Type:     "torrent update",
							ID:       infoHash,
							Name:     t.Info().Name,
							Status:   StatusFailed,
							Progress: 0,
							Speed:    "-",
							ETA:      "--:--",
						})
					} else {
						log.Printf("Upload successful for %s", infoHash)
						nextPhase = StatusTaskCompleted
						q.mu.Lock()
						taskToProcess.Status = nextPhase
						q.mu.Unlock()
						wm.SendProgress(ws.TorrentUpdate{
							Type:     "torrent update",
							ID:       infoHash,
							Name:     t.Info().Name,
							Status:   nextPhase,
							Progress: 100,
							Speed:    "-",
							ETA:      "--:--",
						})
						wm.SendProgress(ws.RefreshUpdate{ // Send refresh only on successful upload
							Type:    "upload refresh",
							Message: "content uploaded on gofile",
						})
					}
				}
			}
		} // End if StatusCompleted for upload

		// Cleanup downloaded files
		if originalPath != "" { // Only attempt removal if path was determined
			log.Printf("Removing downloaded file/folder: %s", originalPath)
			errRemoveOrig := os.RemoveAll(originalPath)
			if errRemoveOrig != nil {
				log.Printf("Failed to delete original path %s for task %s: %s", originalPath, infoHash, errRemoveOrig)
			}

			// If zipped, also remove the zip file if it exists and is different from original
			if taskToProcess.Request.IsZipped && uploadPath == originalPath+".zip" {
				log.Printf("Removing zip file: %s", uploadPath)
				errRemoveZip := os.RemoveAll(uploadPath)
				if errRemoveZip != nil {
					log.Printf("Failed to delete zip path %s for task %s: %s", uploadPath, infoHash, errRemoveZip)
				}
			}
		}
		//  End of cleanup process

		// Remove the task from the main queue slice
		q.mu.Lock()
		log.Printf("Attempting final removal of task %s from queue slice", infoHash)
		q.removeTaskByID_unsafe(infoHash) // Remove by ID now that it's processed
		log.Printf("Current queue length after removal attempt: %d", len(q.tasks))
		q.mu.Unlock()
		//  Task removed

		log.Printf("Finished processing task %s with final status %s", infoHash, nextPhase)
		// The loop will now continue to find the next pending task

	}
}

func getDownloadSpeed(t *torrent.Torrent, duration time.Duration) string {
	// Initial stats
	initialStats := t.Stats()
	initialBytesRead := initialStats.ConnStats.BytesReadData.Int64()

	// Wait for the specified duration
	time.Sleep(duration)

	// gl stats
	finalStats := t.Stats()
	finalBytesRead := finalStats.ConnStats.BytesReadData.Int64()

	// Calculate the difference in bytes read
	bytesRead := finalBytesRead - initialBytesRead

	// Calculate the download speed in bytes per second
	speed := float64(bytesRead) / duration.Seconds()

	return formatSpeed(speed)
}

func calculateETA(t *torrent.Torrent) string {
	// Define a sampling duration
	const sampleDuration = 2 * time.Second

	// Get the initial stats
	initialStats := t.Stats()
	initialBytesRead := initialStats.ConnStats.BytesReadData.Int64()

	// Wait for the sampling duration
	time.Sleep(sampleDuration)

	// Get the final stats
	finalStats := t.Stats()
	finalBytesRead := finalStats.ConnStats.BytesReadData.Int64()

	// Calculate the speed
	bytesRead := finalBytesRead - initialBytesRead
	speed := float64(bytesRead) / sampleDuration.Seconds()

	// Get the completed and total bytes
	bytesCompleted := t.BytesCompleted()
	totalBytes := t.Length()

	// Handle edge cases
	if totalBytes == 0 {
		return "calculating..."
	}
	if speed <= 0 {
		return "calculating..."
	}
	if bytesCompleted >= totalBytes {
		return "complete"
	}

	// Calculate remaining time
	bytesRemaining := totalBytes - bytesCompleted
	seconds := float64(bytesRemaining) / speed
	duration := time.Duration(seconds) * time.Second

	return formatDuration(duration)
}

// formatSpeed converts bytes per second to human readable format
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

// formatDuration formats the ETA in a human readable format
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	} else if d < time.Hour {
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm %ds", m, s)
	} else {
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		return fmt.Sprintf("%dh %dm", h, m)
	}
}

func returnPercentageCompleted(c int64, t int64) float64 {
	percentage := (float64(c) / float64(t)) * 100
	if percentage > 100 {
		percentage = 100
	}
	sizeInMB := float64(t) / 1000000.0
	log.Printf("%.2f%% completed out of %.2f MB", percentage, sizeInMB)

	return math.Round(percentage*100) / 100
}
