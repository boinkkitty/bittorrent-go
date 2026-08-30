package torrent

import (
	"crypto/sha1"
	"fmt"

	"github.com/boinkkitty/bittorrent-go/internal/bencode"
)

type Metadata struct {
	TrackerURL  string
	Length      int
	Hash        [sha1.Size]byte
	PieceLength int
	PieceHashes [][sha1.Size]byte
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

	pieceLength, ok := info["piece length"].(int)
	if !ok {
		return Metadata{}, fmt.Errorf("torrent metadata has invalid info.piece length field")
	}

	pieces, ok := info["pieces"].(string)
	if !ok {
		return Metadata{}, fmt.Errorf("torrent metadata has invalid info.pieces field")
	}
	pieceHashes, err := splitPieceHashes(pieces)
	if err != nil {
		return Metadata{}, err
	}

	return Metadata{
		TrackerURL:  trackerURL,
		Length:      length,
		Hash:        hash,
		PieceLength: pieceLength,
		PieceHashes: pieceHashes,
	}, nil
}

func splitPieceHashes(pieces string) ([][sha1.Size]byte, error) {
	if len(pieces)%sha1.Size != 0 {
		return nil, fmt.Errorf("torrent metadata info.pieces length must be divisible by %d", sha1.Size)
	}

	hashes := make([][sha1.Size]byte, len(pieces)/sha1.Size)
	for i := range hashes {
		start := i * sha1.Size
		copy(hashes[i][:], pieces[start:start+sha1.Size])
	}

	return hashes, nil
}
