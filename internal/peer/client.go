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
	ioTimeout   time.Duration
	peerID      *[sha1.Size]byte
}

func NewClient() *Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	return &Client{
		dialContext: dialer.DialContext,
		random:      rand.Reader,
		ioTimeout:   30 * time.Second,
	}
}

func NewClientWithPeerID(peerID [sha1.Size]byte) *Client {
	client := NewClient()
	client.peerID = &peerID
	return client
}

func (c *Client) Handshake(ctx context.Context, address string, infoHash [sha1.Size]byte) ([sha1.Size]byte, error) {
	conn, remotePeerID, err := c.connect(ctx, address, infoHash)
	if err != nil {
		return [sha1.Size]byte{}, err
	}
	defer conn.Close()
	return remotePeerID, nil
}

func (c *Client) connect(ctx context.Context, address string, infoHash [sha1.Size]byte) (net.Conn, [sha1.Size]byte, error) {
	var localPeerID [sha1.Size]byte
	if c.peerID != nil {
		localPeerID = *c.peerID
	} else {
		if _, err := io.ReadFull(c.random, localPeerID[:]); err != nil {
			return nil, [sha1.Size]byte{}, fmt.Errorf("generate peer ID: %w", err)
		}
	}

	conn, err := c.dialContext(ctx, "tcp", address)
	if err != nil {
		return nil, [sha1.Size]byte{}, fmt.Errorf("dial peer %q: %w", address, err)
	}
	conn = newManagedConn(ctx, conn, c.ioTimeout)

	request := make([]byte, handshakeSize)
	request[0] = byte(len(protocolName))
	copy(request[1:20], protocolName)
	copy(request[28:48], infoHash[:])
	copy(request[48:68], localPeerID[:])
	if err := writeFull(conn, request); err != nil {
		conn.Close()
		return nil, [sha1.Size]byte{}, fmt.Errorf("write peer handshake: %w", err)
	}

	response := make([]byte, handshakeSize)
	if _, err := io.ReadFull(conn, response); err != nil {
		conn.Close()
		return nil, [sha1.Size]byte{}, fmt.Errorf("read peer handshake: %w", err)
	}
	if response[0] != byte(len(protocolName)) {
		conn.Close()
		return nil, [sha1.Size]byte{}, fmt.Errorf("invalid peer handshake protocol length: got %d, want %d", response[0], len(protocolName))
	}
	if !bytes.Equal(response[1:20], []byte(protocolName)) {
		conn.Close()
		return nil, [sha1.Size]byte{}, fmt.Errorf("invalid peer handshake protocol name")
	}
	if !bytes.Equal(response[28:48], infoHash[:]) {
		conn.Close()
		return nil, [sha1.Size]byte{}, fmt.Errorf("peer handshake info hash does not match torrent")
	}

	var remotePeerID [sha1.Size]byte
	copy(remotePeerID[:], response[48:68])
	return conn, remotePeerID, nil
}

type managedConn struct {
	net.Conn
	stopContextWatch func() bool
}

func newManagedConn(ctx context.Context, conn net.Conn, idleTimeout time.Duration) net.Conn {
	timed := &idleTimeoutConn{Conn: conn, timeout: idleTimeout}
	managed := &managedConn{Conn: timed}
	managed.stopContextWatch = context.AfterFunc(ctx, func() { _ = conn.Close() })
	return managed
}

func (c *managedConn) Close() error {
	c.stopContextWatch()
	return c.Conn.Close()
}

type idleTimeoutConn struct {
	net.Conn
	timeout time.Duration
}

func (c *idleTimeoutConn) Read(p []byte) (int, error) {
	if c.timeout > 0 {
		if err := c.SetReadDeadline(time.Now().Add(c.timeout)); err != nil {
			return 0, err
		}
	}
	return c.Conn.Read(p)
}

func (c *idleTimeoutConn) Write(p []byte) (int, error) {
	if c.timeout > 0 {
		if err := c.SetWriteDeadline(time.Now().Add(c.timeout)); err != nil {
			return 0, err
		}
	}
	return c.Conn.Write(p)
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
