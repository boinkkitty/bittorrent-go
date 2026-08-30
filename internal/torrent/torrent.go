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

const maxPieceLength = 64 * 1024 * 1024

func (m Metadata) PieceSize(index int) (int, error) {
	if m.PieceLength <= 0 || m.Length <= 0 {
		return 0, fmt.Errorf("torrent has invalid file or piece length")
	}
	if m.PieceLength > maxPieceLength {
		return 0, fmt.Errorf("torrent piece length %d exceeds supported maximum %d", m.PieceLength, maxPieceLength)
	}
	expectedPieces := 1 + (m.Length-1)/m.PieceLength
	if len(m.PieceHashes) != expectedPieces {
		return 0, fmt.Errorf("torrent metadata has %d piece hashes, want %d", len(m.PieceHashes), expectedPieces)
	}
	if index < 0 || index >= len(m.PieceHashes) {
		return 0, fmt.Errorf("piece index %d out of range [0, %d)", index, len(m.PieceHashes))
	}
	if index < len(m.PieceHashes)-1 {
		return m.PieceLength, nil
	}

	size := m.Length - index*m.PieceLength
	if size <= 0 || size > m.PieceLength {
		return 0, fmt.Errorf("torrent metadata has inconsistent piece count")
	}
	return size, nil
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
