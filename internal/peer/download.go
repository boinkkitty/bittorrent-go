package peer

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"math"
	"net"
)

const (
	messageChoke      byte = 0
	messageUnchoke    byte = 1
	messageInterested byte = 2
	messageBitfield   byte = 5
	messageRequest    byte = 6
	messagePiece      byte = 7

	blockSize     = 16 * 1024
	requestWindow = 5
	maxPieceSize  = 64 * 1024 * 1024
)

func (c *Client) DownloadPiece(
	ctx context.Context,
	address string,
	infoHash [sha1.Size]byte,
	pieceIndex int,
	pieceLength int,
	expectedHash [sha1.Size]byte,
) ([]byte, error) {
	if pieceIndex < 0 || uint64(pieceIndex) > math.MaxUint32 {
		return nil, fmt.Errorf("invalid piece index %d", pieceIndex)
	}
	if pieceLength <= 0 || pieceLength > maxPieceSize {
		return nil, fmt.Errorf("invalid piece length %d", pieceLength)
	}

	conn, _, err := c.connect(ctx, address, infoHash)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := waitForMessage(conn, messageBitfield); err != nil {
		return nil, fmt.Errorf("wait for peer bitfield: %w", err)
	}
	if err := writeMessage(conn, messageInterested, nil); err != nil {
		return nil, fmt.Errorf("send interested message: %w", err)
	}
	if err := waitForMessage(conn, messageUnchoke); err != nil {
		return nil, fmt.Errorf("wait for peer unchoke: %w", err)
	}

	piece := make([]byte, pieceLength)
	pending := make(map[int]int, requestWindow)
	nextBegin := 0
	completed := 0

	fillWindow := func() error {
		for len(pending) < requestWindow && nextBegin < pieceLength {
			length := min(blockSize, pieceLength-nextBegin)
			if err := sendBlockRequest(conn, pieceIndex, nextBegin, length); err != nil {
				return err
			}
			pending[nextBegin] = length
			nextBegin += length
		}
		return nil
	}
	if err := fillWindow(); err != nil {
		return nil, fmt.Errorf("request piece block: %w", err)
	}

	for completed < pieceLength {
		msg, err := readMessage(conn)
		if err != nil {
			return nil, fmt.Errorf("read piece block: %w", err)
		}
		if msg == nil {
			continue
		}
		if msg.ID == messageChoke {
			return nil, fmt.Errorf("peer choked while downloading piece")
		}
		if msg.ID != messagePiece {
			continue
		}

		begin, block, err := parsePieceBlock(msg.Payload, pieceIndex, pieceLength, pending)
		if err != nil {
			return nil, err
		}
		copy(piece[begin:begin+len(block)], block)
		delete(pending, begin)
		completed += len(block)
		if err := fillWindow(); err != nil {
			return nil, fmt.Errorf("request piece block: %w", err)
		}
	}

	actualHash := sha1.Sum(piece)
	if actualHash != expectedHash {
		return nil, fmt.Errorf("piece %d hash mismatch: got %x, want %x", pieceIndex, actualHash, expectedHash)
	}
	return piece, nil
}

func waitForMessage(conn net.Conn, wantedID byte) error {
	for {
		msg, err := readMessage(conn)
		if err != nil {
			return err
		}
		if msg != nil && msg.ID == wantedID {
			return nil
		}
	}
}

func sendBlockRequest(conn net.Conn, index, begin, length int) error {
	var payload [12]byte
	binary.BigEndian.PutUint32(payload[0:4], uint32(index))
	binary.BigEndian.PutUint32(payload[4:8], uint32(begin))
	binary.BigEndian.PutUint32(payload[8:12], uint32(length))
	return writeMessage(conn, messageRequest, payload[:])
}

func parsePieceBlock(payload []byte, wantIndex, pieceLength int, pending map[int]int) (int, []byte, error) {
	if len(payload) < 8 {
		return 0, nil, fmt.Errorf("piece message payload is too short: %d", len(payload))
	}
	index := int(binary.BigEndian.Uint32(payload[0:4]))
	if index != wantIndex {
		return 0, nil, fmt.Errorf("piece message index = %d, want piece index %d", index, wantIndex)
	}
	begin := int(binary.BigEndian.Uint32(payload[4:8]))
	expectedLength, ok := pending[begin]
	if !ok {
		return 0, nil, fmt.Errorf("unexpected or duplicate piece block offset %d", begin)
	}
	block := payload[8:]
	if len(block) != expectedLength {
		return 0, nil, fmt.Errorf("piece block at offset %d has length %d, want %d", begin, len(block), expectedLength)
	}
	if begin < 0 || begin > pieceLength-len(block) {
		return 0, nil, fmt.Errorf("piece block at offset %d exceeds piece length %d", begin, pieceLength)
	}
	return begin, bytes.Clone(block), nil
}
