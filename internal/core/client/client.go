package client

import (
	"github.com/anacrolix/torrent"
)

func New(cfg *torrent.ClientConfig) (cl *torrent.Client, e error) {
	return torrent.NewClient(cfg)
}
