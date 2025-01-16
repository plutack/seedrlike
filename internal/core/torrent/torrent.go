package torrent

import (
	"github.com/anacrolix/torrent"
	"github.com/plutack/seedrlike/internal/core/client"
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

func newFile(name string, hash string, size int64, status string) TorrentFile {
	return
}

func CreateDownloadTask(mLink magnetLink) (TorrentFile, error) {
	var err error
	if seedrlikeTorrentClient == nil {
		seedrlikeTorrentClient, err = client.New(nil)
	}
	if err != nil {
		return TorrentFile{}, err
	}
	torrent, err := seedrlikeTorrentClient.AddMagnet(mLink)
	if err != nil {
		return TorrentFile{}, err
	}
	<-torrent.GotInfo()
	fileInfo := torrent.Info()
	return TorrentFile{fileInfo.Name, torrent.InfoHash().AsString(), fileInfo.Length, StatusPending}, nil
}
