package queue

import (
	"bytes"
	"errors"
	"math"
	"strings"

	"github.com/anacrolix/torrent/metainfo"
)

const MaxTorrentFileSize = 4 << 20

// ParseTorrentFile validates uploaded metadata without contacting trackers or peers.
// The durable queue currently identifies jobs by their v1 hash; hybrid torrents
// work too, but pure v2 torrents are explicitly rejected rather than misidentified.
func ParseTorrentFile(data []byte) (*metainfo.MetaInfo, string, error) {
	invalid := errors.New("invalid torrent file")
	if len(data) == 0 || len(data) > MaxTorrentFileSize {
		return nil, "", invalid
	}
	mi, err := metainfo.Load(bytes.NewReader(data))
	if err != nil {
		return nil, "", invalid
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		return nil, "", invalid
	}
	if !info.HasV1() {
		return nil, "", errors.New("v2-only torrent files are not supported")
	}
	if info.PieceLength <= 0 || len(info.Pieces)%20 != 0 || len(info.Files) > 10000 {
		return nil, "", invalid
	}
	if _, err = safeRelative(info.BestName()); err != nil {
		return nil, "", invalid
	}
	total := info.Length
	if len(info.Files) > 0 {
		if info.Length != 0 {
			return nil, "", invalid
		}
		total = 0
		seen := map[string]bool{}
		for _, f := range info.Files {
			p := strings.Join(f.BestPath(), "/")
			if len(f.BestPath()) == 0 {
				return nil, "", invalid
			}
			for _, part := range f.BestPath() {
				if part == "" || part == "." || part == ".." || strings.ContainsAny(part, "/\\") {
					return nil, "", invalid
				}
			}
			if _, err = safeRelative(p); err != nil || seen[p] || f.Length < 0 || f.Length > math.MaxInt64-total {
				return nil, "", invalid
			}
			seen[p] = true
			total += f.Length
		}
	}
	if total < 0 {
		return nil, "", invalid
	}
	pieces := total / info.PieceLength
	if total%info.PieceLength != 0 {
		pieces++
	}
	if int64(len(info.Pieces)/20) != pieces {
		return nil, "", invalid
	}
	return mi, mi.Magnet(nil, &info).String(), nil
}
