package queue

import (
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/plutack/go-gofile/api"
	"github.com/plutack/seedrlike/internal/core/upload"
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
func ProcessTasks(c *torrent.Client, q *DownloadQueue, u *api.Api, r string, db *database.Queries) {
	for {
		l := <-q.tasks
		log.Println("New magnet link marked for download")
		t, err := c.AddMagnet(l)
		if err != nil {
			log.Println("error adding link to client for download")
			continue
		}
		if _, ok := <-t.GotInfo(); !ok {
			// check if file exists in database so we don't waste bandwidth
			// and handle appropiately
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
			availableServerInfo, err := u.GetAvailableServers("eu")
			if err != nil {
				panic(err)
			}
			euServer := availableServerInfo.Data.Servers[0].Name
			err = upload.SendFolderToServer(getFolderPath(t.Info().Name), u, r, euServer, t.InfoHash().String(), db)
			if err != nil {
				log.Printf("failed to upload %s to gofile: %s", t.Info().Name, err)
			}
			// TODO: delete folder from host system
			err = os.RemoveAll(getFolderPath(t.Info().Name))
			if err != nil {
				log.Printf("failed to delete %s  from host: %s", t.Info().Name, err)
			}
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
