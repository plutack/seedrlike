package torrent

import (
	"fmt"

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

func CreateDownloadTask(mLink magnetLink) (TorrentFile, error) {
	var err error

	config := torrent.NewDefaultClientConfig()
	config.DataDir = "/home/plutack/Downloads/seedrlike"
	if seedrlikeTorrentClient == nil {
		seedrlikeTorrentClient, err = client.New(config)
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
	fmt.Println(fileInfo)
	torrent.DownloadAll()
	return TorrentFile{fileInfo.Name, torrent.InfoHash().AsString(), fileInfo.Length, StatusPending}, nil
}
