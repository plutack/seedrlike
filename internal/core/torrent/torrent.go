package torrent

// NOTE: see downloadResponse
type TorrentFile struct {
	Name   string
	Hash   string
	Size   int64
	Status string
}

// NOTE:  this is suppose to be for the frontend updates
// websocket transmission perhaps?
const (
	StatusPending   = "pending"
	StatusStarted   = "started"
	StatusCompleted = "completed"
)
