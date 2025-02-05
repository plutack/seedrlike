package response

import "github.com/plutack/seedrlike/internal/core/torrent"

// NOTE: might need to change the type of Response to what was created by sqlc
type DownloadResponse struct {
	Message  string `json:"message"`
	Response torrent.TorrentFile
}

type GetDownloadsResponse struct {
	Message   string `json:"message"`
	Downloads []torrent.TorrentFile
}

type StopDownloadTaskResponse struct {
	Message string `json:"message"`
}

func (r DownloadResponse) String() string {
	return r.Message
}

func (r GetDownloadsResponse) String() string {
	return r.Message
}

func (r StopDownloadTaskResponse) String() string {
	return r.Message
}
