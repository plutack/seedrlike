package queue

import (
	"fmt"
	"log"

	"github.com/anacrolix/torrent"
)

type (
	magnetLink = string

	DownloadQueue struct {
		tasks chan magnetLink
	}
)

func New() *DownloadQueue {
	return &DownloadQueue{
		tasks: make(chan magnetLink),
	}
}

func (q *DownloadQueue) Add(m magnetLink) {
	q.tasks <- m
	log.Println("")
}

func ProcessTasks(c *torrent.Client, q *DownloadQueue) {
	for {
		l := <-q.tasks
		log.Println("New magnet link marked for download")
		t, err := c.AddMagnet(l)
		if err != nil {
			log.Println("error adding link to client for download")
		}
		if _, ok := <-t.GotInfo(); !ok {
			fileInfo := t.Info()
			fmt.Println(fileInfo)
			t.DownloadAll()
			log.Printf("%s started downloading", t.Name)
			t.DisallowDataUpload()
			for !c.WaitAll() {

				completed := t.Stats().BytesRead

				printPercentageCompleted(float64(completed.Int64()), t.Length())
			}
			log.Printf("File name: %s downloaded completely", t.Name())
			t.Drop()
		}
	}
}

func printPercentageCompleted(c float64, t int64) {
	log.Printf("%f.2 completed out of %d MB", (c/float64(t))*100, t/1000)
}
