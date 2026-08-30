package torrent

import (
	"fmt"

	"github.com/boinkkitty/bittorrent-go/internal/bencode"
)

type Metadata struct {
	TrackerURL string
	Length     int
}

func Parse(data []byte) (Metadata, error) {
	decoded, err := bencode.Decode(string(data))
	if err != nil {
		return Metadata{}, err
	}

	root, ok := decoded.(map[string]any)
	if !ok {
		return Metadata{}, fmt.Errorf("torrent metadata must be a dictionary")
	}

	trackerURL, ok := root["announce"].(string)
	if !ok {
		return Metadata{}, fmt.Errorf("torrent metadata has invalid announce field")
	}

	info, ok := root["info"].(map[string]any)
	if !ok {
		return Metadata{}, fmt.Errorf("torrent metadata has invalid info field")
	}

	length, ok := info["length"].(int)
	if !ok {
		return Metadata{}, fmt.Errorf("torrent metadata has invalid info.length field")
	}

	return Metadata{
		TrackerURL: trackerURL,
		Length:     length,
	}, nil
}
