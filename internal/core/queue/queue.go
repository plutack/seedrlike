package queue

import (
	"errors"
	"log"
	"time"

	"github.com/anacrolix/torrent"
)

type (
	magnetLink = string

	DownloadQueue struct {
		tasks chan magnetLink
	}
)

var errorQueueFull = errors.New("Download Queue full")

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

func ProcessTasks(c *torrent.Client, q *DownloadQueue) {
	for {
		l := <-q.tasks
		log.Println("New magnet link marked for download")
		t, err := c.AddMagnet(l)
		if err != nil {
			log.Println("error adding link to client for download")
			continue
		}
		if _, ok := <-t.GotInfo(); !ok {
			t.DownloadAll()
			log.Printf("%s started downloading", t.Info().Name)
			t.DisallowDataUpload()
			// TODO: this should eventually become a websocket to the frontend
			for {
				if t.Complete().Bool() {
					break
				}
				completed := t.Stats().BytesRead
				printPercentageCompleted(completed.Int64(), t.Length())
			}
			log.Printf("File name: %s downloaded completely", t.Name())
			t.Drop()
		}
	}
}

func printPercentageCompleted(c int64, t int64) {
	time.Sleep(2 * time.Second)
	percentage := (float64(c) / float64(t)) * 100
	if percentage > 100 {
		percentage = 100
	}
	sizeInMB := float64(t) / 1000000.0
	log.Printf("%.2f%% completed out of %.2f MB", percentage, sizeInMB)
}
