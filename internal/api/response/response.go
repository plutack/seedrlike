package response

import "github.com/plutack/seedrlike/internal/torrent"

type DownloadResponse struct {
	Message string `json:"message"`
}

type GetDownloadsResponse struct {
	Message   string `json:"message"`
	Downloads []torrent.Torrent
}

func (r DownloadResponse) String() string {
	return r.Message
}

func (r GetDownloadsResponse) String() string {
	return r.Message
}
