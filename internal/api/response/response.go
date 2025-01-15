package response

import "github.com/plutack/seedrlike/internal/torrent"

type DownloadResponse struct {
	Message string `json:"message"`
}

type GetDownloadsResponse struct {
	Message   string `json:"message"`
	Downloads []torrent.Torrent
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
