package torrent

import (
	"github.com/anacrolix/torrent"
	"github.com/plutack/seedrlike/internal/core/client"
)

type TorrentFile struct {
	name   string
	hash   string
	size   int64
	status string
}

const (
	StatusPending   = "pending"
	StatusStarted   = "started"
	StatusCompleted = "completed"
)

type magnetLink = string

var seedrlikeTorrentClient *torrent.Client

func newFile(name string, hash string, size int64, status string) *TorrentFile {
	return &TorrentFile{name, hash, size, status}
}

func CreateDownloadTask(mLink magnetLink) (*TorrentFile, error) {
	var err error
	if seedrlikeTorrentClient == nil {
		seedrlikeTorrentClient, err = client.New(nil)
	}
	if err != nil {
		return nil, err
	}
	torrent, err := seedrlikeTorrentClient.AddMagnet(mLink)
	var tFile *TorrentFile
	if err != nil {
		return nil, err
	}
	<-torrent.GotInfo()
	fileInfo := torrent.Info()
	tFile = newFile(fileInfo.Name, torrent.InfoHash().AsString(), fileInfo.Length, StatusPending)
	return tFile, nil
}
