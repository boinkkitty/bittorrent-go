package peer

import (
	"encoding/binary"
	"fmt"
	"io"
)

const maxMessageSize uint32 = 1 << 20

type message struct {
	ID      byte
	Payload []byte
}

func readMessage(r io.Reader) (*message, error) {
	var prefix [4]byte
	if _, err := io.ReadFull(r, prefix[:]); err != nil {
		return nil, fmt.Errorf("read message length: %w", err)
	}
	length := binary.BigEndian.Uint32(prefix[:])
	if length == 0 {
		return nil, nil
	}
	if length > maxMessageSize {
		return nil, fmt.Errorf("peer message length %d is too large", length)
	}

	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, fmt.Errorf("read message body: %w", err)
	}
	return &message{ID: body[0], Payload: body[1:]}, nil
}

func writeMessage(w io.Writer, id byte, payload []byte) error {
	length := 1 + len(payload)
	if uint64(length) > uint64(maxMessageSize) {
		return fmt.Errorf("peer message length %d is too large", length)
	}

	wire := make([]byte, 4+length)
	binary.BigEndian.PutUint32(wire[:4], uint32(length))
	wire[4] = id
	copy(wire[5:], payload)
	return writeFull(w, wire)
}
