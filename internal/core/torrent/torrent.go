package torrent

import (
	"github.com/anacrolix/torrent"
	"github.com/plutack/seedrlike/internal/core/client"
	"github.com/plutack/seedrlike/internal/core/queue"
)

type TorrentFile struct {
	Name   string
	Hash   string
	Size   int64
	Status string
}

const (
	StatusPending   = "pending"
	StatusStarted   = "started"
	StatusCompleted = "completed"
)

type magnetLink = string

var seedrlikeTorrentClient *torrent.Client

var storagePath = "/home/plutack/Downloads/seedrlike"

func CreateDownloadTask(mLink magnetLink, q queue.DownloadQueue) error {
	var err error
	config := torrent.NewDefaultClientConfig()
	config.DataDir = storagePath
	if seedrlikeTorrentClient == nil {
		seedrlikeTorrentClient, err = client.New(config)
	}
	if err != nil {
		return err
	}
	q.Add(mLink)
	return nil
}
