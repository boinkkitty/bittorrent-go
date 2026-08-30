package torrent

import (
	"crypto/sha1"
	"fmt"

	"github.com/boinkkitty/bittorrent-go/internal/bencode"
)

type Metadata struct {
	TrackerURL string
	Length     int
	Hash       [20]byte
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

	encodedInfo, err := bencode.Encode(info)
	if err != nil {
		return Metadata{}, fmt.Errorf("encode torrent info: %w", err)
	}
	hash := sha1.Sum(encodedInfo)

	return Metadata{
		TrackerURL: trackerURL,
		Length:     length,
		Hash:       hash,
	}, nil
}
