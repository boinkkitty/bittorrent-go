package peer

import (
	"bytes"
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
)

func TestClientHandshakeExchangesProtocolMessage(t *testing.T) {
	infoHash := [sha1.Size]byte{
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a,
		0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14,
	}
	localPeerID := [sha1.Size]byte{
		0x14, 0x13, 0x12, 0x11, 0x10, 0x0f, 0x0e, 0x0d, 0x0c, 0x0b,
		0x0a, 0x09, 0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01,
	}
	remotePeerID := [sha1.Size]byte{
		0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x29, 0x2a,
		0x2b, 0x2c, 0x2d, 0x2e, 0x2f, 0x30, 0x31, 0x32, 0x33, 0x34,
	}

	clientConn, peerConn := net.Pipe()
	t.Cleanup(func() {
		clientConn.Close()
		peerConn.Close()
	})
	client := &Client{
		dialContext: func(_ context.Context, network, address string) (net.Conn, error) {
			if network != "tcp" {
				return nil, fmt.Errorf("network = %q, want tcp", network)
			}
			if address != "192.0.2.10:6881" {
				return nil, fmt.Errorf("address = %q, want 192.0.2.10:6881", address)
			}
			return clientConn, nil
		},
		random: bytes.NewReader(localPeerID[:]),
	}

	peerResult := make(chan error, 1)
	go func() {
		request := make([]byte, 68)
		if _, err := io.ReadFull(peerConn, request); err != nil {
			peerResult <- fmt.Errorf("read request: %w", err)
			return
		}

		wantRequest := make([]byte, 68)
		wantRequest[0] = 19
		copy(wantRequest[1:20], "BitTorrent protocol")
		copy(wantRequest[28:48], infoHash[:])
		copy(wantRequest[48:68], localPeerID[:])
		if !bytes.Equal(request, wantRequest) {
			peerResult <- fmt.Errorf("request = %x, want %x", request, wantRequest)
			return
		}

		response := append([]byte(nil), wantRequest...)
		copy(response[48:68], remotePeerID[:])
		_, err := peerConn.Write(response)
		peerResult <- err
	}()

	gotPeerID, err := client.Handshake(context.Background(), "192.0.2.10:6881", infoHash)
	if err != nil {
		t.Fatalf("Handshake() error = %v", err)
	}
	if gotPeerID != remotePeerID {
		t.Fatalf("Handshake() peer ID = %x, want %x", gotPeerID, remotePeerID)
	}
	if err := <-peerResult; err != nil {
		t.Fatalf("peer exchange: %v", err)
	}
}

func TestClientHandshakeRejectsInvalidResponses(t *testing.T) {
	infoHash := [sha1.Size]byte{1, 2, 3, 4, 5}
	remotePeerID := [sha1.Size]byte{20, 19, 18, 17, 16}

	tests := []struct {
		name      string
		mutate    func([]byte)
		wantError string
	}{
		{
			name:      "invalid protocol length",
			mutate:    func(response []byte) { response[0] = 18 },
			wantError: "protocol length",
		},
		{
			name:      "invalid protocol name",
			mutate:    func(response []byte) { response[1] = 'X' },
			wantError: "protocol name",
		},
		{
			name:      "mismatched info hash",
			mutate:    func(response []byte) { response[28] ^= 0xff },
			wantError: "info hash",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := testHandshake(infoHash, remotePeerID)
			tt.mutate(response)
			client, peerResult := clientRespondingWith(t, response)

			_, err := client.Handshake(context.Background(), "192.0.2.10:6881", infoHash)
			if err == nil {
				t.Fatal("Handshake() error = nil, want an error")
			}
			if !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("Handshake() error = %q, want it to contain %q", err, tt.wantError)
			}
			if err := <-peerResult; err != nil {
				t.Fatalf("peer exchange: %v", err)
			}
		})
	}
}

func TestClientHandshakeReportsTruncatedResponse(t *testing.T) {
	infoHash := [sha1.Size]byte{1, 2, 3, 4, 5}
	response := testHandshake(infoHash, [sha1.Size]byte{})[:67]
	client, peerResult := clientRespondingWith(t, response)

	_, err := client.Handshake(context.Background(), "192.0.2.10:6881", infoHash)
	if err == nil {
		t.Fatal("Handshake() error = nil, want an error")
	}
	if !strings.Contains(err.Error(), "read peer handshake") {
		t.Fatalf("Handshake() error = %q, want it to contain %q", err, "read peer handshake")
	}
	if err := <-peerResult; err != nil {
		t.Fatalf("peer exchange: %v", err)
	}
}

func TestClientHandshakeReportsPeerIDGenerationFailure(t *testing.T) {
	client := &Client{
		random: errorReader{err: io.ErrUnexpectedEOF},
		dialContext: func(context.Context, string, string) (net.Conn, error) {
			t.Fatal("dialContext called after peer ID generation failed")
			return nil, nil
		},
	}

	_, err := client.Handshake(context.Background(), "192.0.2.10:6881", [sha1.Size]byte{})
	if err == nil {
		t.Fatal("Handshake() error = nil, want an error")
	}
	if !strings.Contains(err.Error(), "generate peer ID") {
		t.Fatalf("Handshake() error = %q, want it to contain %q", err, "generate peer ID")
	}
}

func TestClientHandshakeReportsDialFailure(t *testing.T) {
	client := &Client{
		random: bytes.NewReader(make([]byte, sha1.Size)),
		dialContext: func(context.Context, string, string) (net.Conn, error) {
			return nil, fmt.Errorf("connection refused")
		},
	}

	_, err := client.Handshake(context.Background(), "192.0.2.10:6881", [sha1.Size]byte{})
	if err == nil {
		t.Fatal("Handshake() error = nil, want an error")
	}
	if !strings.Contains(err.Error(), "dial peer \"192.0.2.10:6881\"") {
		t.Fatalf("Handshake() error = %q, want dial context", err)
	}
}

func testHandshake(infoHash, peerID [sha1.Size]byte) []byte {
	response := make([]byte, 68)
	response[0] = 19
	copy(response[1:20], "BitTorrent protocol")
	copy(response[28:48], infoHash[:])
	copy(response[48:68], peerID[:])
	return response
}

func clientRespondingWith(t *testing.T, response []byte) (*Client, <-chan error) {
	t.Helper()
	clientConn, peerConn := net.Pipe()
	t.Cleanup(func() {
		clientConn.Close()
		peerConn.Close()
	})

	peerResult := make(chan error, 1)
	go func() {
		request := make([]byte, 68)
		if _, err := io.ReadFull(peerConn, request); err != nil {
			peerResult <- fmt.Errorf("read request: %w", err)
			return
		}
		if err := writeFull(peerConn, response); err != nil {
			peerResult <- fmt.Errorf("write response: %w", err)
			return
		}
		peerConn.Close()
		peerResult <- nil
	}()

	return &Client{
		dialContext: func(context.Context, string, string) (net.Conn, error) {
			return clientConn, nil
		},
		random: bytes.NewReader(make([]byte, sha1.Size)),
	}, peerResult
}

type errorReader struct {
	err error
}

func (r errorReader) Read([]byte) (int, error) {
	return 0, r.err
}
