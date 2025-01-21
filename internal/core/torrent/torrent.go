package torrent

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
