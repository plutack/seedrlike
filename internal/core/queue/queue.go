package queue

import (
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/plutack/go-gofile/api"
	"github.com/plutack/seedrlike/internal/core/upload"
	ws "github.com/plutack/seedrlike/internal/core/websocket"
	database "github.com/plutack/seedrlike/internal/database/sqlc"
)

type (
	magnetLink = string

	DownloadQueue struct {
		tasks chan magnetLink
	}
)

var (
	errorQueueFull = errors.New("Download Queue full")
	storagePath    = "/home/plutack/Downloads/seedrlike"
)

func New() *DownloadQueue {
	return &DownloadQueue{
		tasks: make(chan magnetLink, 50), // allow for 50 additions to the queue. anymore will result to a server busy status code client side
	}
}

func (q *DownloadQueue) Add(m magnetLink) error {
	select {
	case q.tasks <- m:
		log.Println("Link added successfully")
		return nil
	default:
		log.Println("Download Queue full")
		return errorQueueFull
	}
}

func getFolderPath(folderName string) string {
	return fmt.Sprintf("%s/%s", storagePath, folderName)
}
func ProcessTasks(c *torrent.Client, q *DownloadQueue, u *api.Api, r string, db *database.Queries, wm *ws.WebsocketManager) {
	for {
		l := <-q.tasks
		log.Println("New magnet link marked for download")
		t, err := c.AddMagnet(l)
		if err != nil {
			log.Println("error adding link to client for download")
			continue
		}

		// Initial "pending" update
		wm.SendProgress(ws.TorrentUpdate{
			Type:     "torrent update",
			ID:       t.InfoHash().String(),
			Name:     "unknown",
			Status:   "pending",
			Progress: 0,
			Speed:    "0",
			ETA:      "calculating...",
		})

		if _, ok := <-t.GotInfo(); !ok {
			t.DownloadAll()
			log.Printf("%s started downloading", t.Info().Name)

			t.DisallowDataUpload()

			// Channel to stop Goroutines once complete
			stopChan := make(chan struct{})

			// Start Goroutine for speed and ETA updates
			go func() {
				for {
					select {
					case <-stopChan:
						return
					default:
						speed := getDownloadSpeed(t, 2*time.Second)
						eta := calculateETA(t)
						wm.SendProgress(ws.TorrentUpdate{
							Type:     "torrent update",
							ID:       t.InfoHash().String(),
							Name:     t.Info().Name,
							Status:   "downloading",
							Progress: returnPercentageCompleted(t.BytesCompleted(), t.Length()),
							Speed:    speed,
							ETA:      eta,
						})
						time.Sleep(2 * time.Second) // Adjust interval as needed
					}
				}
			}()

			// Wait until torrent is complete
			for !t.Complete().Bool() {
				time.Sleep(1 * time.Second)
			}

			// Stop the update Goroutine
			close(stopChan)

			// Final update
			wm.SendProgress(ws.TorrentUpdate{
				Type:     "torrent update",
				ID:       t.InfoHash().String(),
				Name:     t.Info().Name,
				Status:   "completed",
				Progress: 100,
				Speed:    "0",
				ETA:      "done",
			})

			log.Printf("File name: %s downloaded completely", t.Name())
			t.Drop()

			// Upload and cleanup
			availableServerInfo, err := u.GetAvailableServers("eu")
			if err != nil {
				panic(err)
			}
			euServer := availableServerInfo.Data.Servers[0].Name
			err = upload.SendFolderToServer(getFolderPath(t.Info().Name), u, r, euServer, t.InfoHash().String(), db)
			if err != nil {
				log.Printf("failed to upload %s to gofile: %s", t.Info().Name, err)
			}

			wm.SendProgress(ws.RefreshUpdate{
				Type:    "upload refresh",
				Message: "file uploaded on gofile",
			})

			err = os.RemoveAll(getFolderPath(t.Info().Name))
			if err != nil {
				log.Printf("failed to delete %s from host: %s", t.Info().Name, err)
			}
		}
	}
}

func getDownloadSpeed(t *torrent.Torrent, duration time.Duration) string {
	// Initial stats
	initialStats := t.Stats()
	initialBytesRead := initialStats.ConnStats.BytesReadData.Int64()

	// Wait for the specified duration
	time.Sleep(duration)

	// Final stats
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
