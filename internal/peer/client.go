package peer

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"fmt"
	"io"
	"net"
	"time"
)

const (
	protocolName  = "BitTorrent protocol"
	handshakeSize = 1 + len(protocolName) + 8 + sha1.Size + sha1.Size
)

type dialContextFunc func(context.Context, string, string) (net.Conn, error)

type Client struct {
	dialContext dialContextFunc
	random      io.Reader
}

func NewClient() *Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	return &Client{
		dialContext: dialer.DialContext,
		random:      rand.Reader,
	}
}

func (c *Client) Handshake(ctx context.Context, address string, infoHash [sha1.Size]byte) ([sha1.Size]byte, error) {
	var localPeerID [sha1.Size]byte
	if _, err := io.ReadFull(c.random, localPeerID[:]); err != nil {
		return [sha1.Size]byte{}, fmt.Errorf("generate peer ID: %w", err)
	}

	conn, err := c.dialContext(ctx, "tcp", address)
	if err != nil {
		return [sha1.Size]byte{}, fmt.Errorf("dial peer %q: %w", address, err)
	}
	defer conn.Close()

	request := make([]byte, handshakeSize)
	request[0] = byte(len(protocolName))
	copy(request[1:20], protocolName)
	copy(request[28:48], infoHash[:])
	copy(request[48:68], localPeerID[:])
	if err := writeFull(conn, request); err != nil {
		return [sha1.Size]byte{}, fmt.Errorf("write peer handshake: %w", err)
	}

	response := make([]byte, handshakeSize)
	if _, err := io.ReadFull(conn, response); err != nil {
		return [sha1.Size]byte{}, fmt.Errorf("read peer handshake: %w", err)
	}
	if response[0] != byte(len(protocolName)) {
		return [sha1.Size]byte{}, fmt.Errorf("invalid peer handshake protocol length: got %d, want %d", response[0], len(protocolName))
	}
	if !bytes.Equal(response[1:20], []byte(protocolName)) {
		return [sha1.Size]byte{}, fmt.Errorf("invalid peer handshake protocol name")
	}
	if !bytes.Equal(response[28:48], infoHash[:]) {
		return [sha1.Size]byte{}, fmt.Errorf("peer handshake info hash does not match torrent")
	}

	var remotePeerID [sha1.Size]byte
	copy(remotePeerID[:], response[48:68])
	return remotePeerID, nil
}

func writeFull(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
