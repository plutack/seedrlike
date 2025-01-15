package torrent

type Torrent struct {
	name   string
	hash   string
	size   float64
	status string
}

func New(name string, hash string, size float64, status string) *Torrent {
	return &Torrent{name, hash, size, status}
}
