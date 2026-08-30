package peer

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"
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

func TestClientDownloadPiecePipelinesRequestsAndAssemblesOutOfOrderBlocks(t *testing.T) {
	const blockSize = 16 * 1024
	infoHash := [sha1.Size]byte{1, 2, 3, 4, 5}
	pieceIndex := 3
	pieceData := make([]byte, 5*blockSize+123)
	for i := range pieceData {
		pieceData[i] = byte(i % 251)
	}
	expectedHash := sha1.Sum(pieceData)

	clientConn, peerConn := net.Pipe()
	t.Cleanup(func() {
		clientConn.Close()
		peerConn.Close()
	})
	client := &Client{
		dialContext: func(context.Context, string, string) (net.Conn, error) {
			return clientConn, nil
		},
		random: bytes.NewReader(make([]byte, sha1.Size)),
	}

	peerResult := make(chan error, 1)
	go func() {
		if err := serveHandshake(peerConn, infoHash); err != nil {
			peerResult <- err
			return
		}
		if err := writeMessage(peerConn, 5, []byte{0xff}); err != nil {
			peerResult <- fmt.Errorf("write bitfield: %w", err)
			return
		}
		interested, err := readMessage(peerConn)
		if err != nil {
			peerResult <- fmt.Errorf("read interested: %w", err)
			return
		}
		if interested == nil || interested.ID != 2 || len(interested.Payload) != 0 {
			peerResult <- fmt.Errorf("interested message = %+v, want ID 2 with empty payload", interested)
			return
		}
		if err := writeFull(peerConn, []byte{0, 0, 0, 0}); err != nil {
			peerResult <- fmt.Errorf("write keep-alive: %w", err)
			return
		}
		if err := writeMessage(peerConn, 1, nil); err != nil {
			peerResult <- fmt.Errorf("write unchoke: %w", err)
			return
		}

		requests := make(map[int]int)
		for range 5 {
			begin, length, err := readPieceRequest(peerConn, pieceIndex)
			if err != nil {
				peerResult <- err
				return
			}
			requests[begin] = length
		}
		for begin := 0; begin < 5*blockSize; begin += blockSize {
			if got := requests[begin]; got != blockSize {
				peerResult <- fmt.Errorf("request begin %d length = %d, want %d", begin, got, blockSize)
				return
			}
		}

		if err := writePieceBlock(peerConn, pieceIndex, 4*blockSize, pieceData[4*blockSize:5*blockSize]); err != nil {
			peerResult <- err
			return
		}
		begin, length, err := readPieceRequest(peerConn, pieceIndex)
		if err != nil {
			peerResult <- err
			return
		}
		if begin != 5*blockSize || length != 123 {
			peerResult <- fmt.Errorf("final request = begin %d length %d, want begin %d length 123", begin, length, 5*blockSize)
			return
		}

		for _, blockBegin := range []int{2 * blockSize, 0, 5 * blockSize, blockSize, 3 * blockSize} {
			blockEnd := min(blockBegin+blockSize, len(pieceData))
			if err := writePieceBlock(peerConn, pieceIndex, blockBegin, pieceData[blockBegin:blockEnd]); err != nil {
				peerResult <- err
				return
			}
		}
		peerResult <- nil
	}()

	got, err := client.DownloadPiece(context.Background(), "192.0.2.10:6881", infoHash, pieceIndex, len(pieceData), expectedHash)
	if err != nil {
		t.Fatalf("DownloadPiece() error = %v", err)
	}
	if !bytes.Equal(got, pieceData) {
		t.Fatalf("DownloadPiece() returned incorrect piece data")
	}
	if err := <-peerResult; err != nil {
		t.Fatalf("peer exchange: %v", err)
	}
}

func TestClientDownloadPieceRejectsHashMismatch(t *testing.T) {
	infoHash := [sha1.Size]byte{1, 2, 3}
	block := []byte("piece data")
	client, peerResult := singleBlockPeer(t, infoHash, 0, 0, block)

	_, err := client.DownloadPiece(context.Background(), "192.0.2.10:6881", infoHash, 0, len(block), sha1.Sum([]byte("different data")))
	if err == nil {
		t.Fatal("DownloadPiece() error = nil, want an error")
	}
	if !strings.Contains(err.Error(), "hash") {
		t.Fatalf("DownloadPiece() error = %q, want it to contain %q", err, "hash")
	}
	if err := <-peerResult; err != nil {
		t.Fatalf("peer exchange: %v", err)
	}
}

func TestClientDownloadPieceRejectsBlockForDifferentPiece(t *testing.T) {
	infoHash := [sha1.Size]byte{1, 2, 3}
	block := []byte("piece data")
	client, peerResult := singleBlockPeer(t, infoHash, 1, 0, block)

	_, err := client.DownloadPiece(context.Background(), "192.0.2.10:6881", infoHash, 0, len(block), sha1.Sum(block))
	if err == nil {
		t.Fatal("DownloadPiece() error = nil, want an error")
	}
	if !strings.Contains(err.Error(), "piece index") {
		t.Fatalf("DownloadPiece() error = %q, want it to contain %q", err, "piece index")
	}
	if err := <-peerResult; err != nil {
		t.Fatalf("peer exchange: %v", err)
	}
}

func TestClientDownloadPieceRejectsExcessivePieceLengthBeforeDialing(t *testing.T) {
	client := &Client{
		dialContext: func(context.Context, string, string) (net.Conn, error) {
			t.Fatal("dialContext called for excessive piece length")
			return nil, nil
		},
		random: bytes.NewReader(make([]byte, sha1.Size)),
	}

	_, err := client.DownloadPiece(
		context.Background(),
		"192.0.2.10:6881",
		[sha1.Size]byte{},
		0,
		64*1024*1024+1,
		[sha1.Size]byte{},
	)
	if err == nil {
		t.Fatal("DownloadPiece() error = nil, want excessive piece length error")
	}
}

func singleBlockPeer(t *testing.T, infoHash [sha1.Size]byte, responseIndex, responseBegin int, block []byte) (*Client, <-chan error) {
	t.Helper()
	clientConn, peerConn := net.Pipe()
	t.Cleanup(func() {
		clientConn.Close()
		peerConn.Close()
	})
	peerResult := make(chan error, 1)
	go func() {
		if err := serveHandshake(peerConn, infoHash); err != nil {
			peerResult <- err
			return
		}
		if err := writeMessage(peerConn, 5, []byte{0xff}); err != nil {
			peerResult <- err
			return
		}
		if _, err := readMessage(peerConn); err != nil {
			peerResult <- err
			return
		}
		if err := writeMessage(peerConn, 1, nil); err != nil {
			peerResult <- err
			return
		}
		if _, _, err := readPieceRequest(peerConn, 0); err != nil {
			peerResult <- err
			return
		}
		peerResult <- writePieceBlock(peerConn, responseIndex, responseBegin, block)
	}()
	return &Client{
		dialContext: func(context.Context, string, string) (net.Conn, error) { return clientConn, nil },
		random:      bytes.NewReader(make([]byte, sha1.Size)),
	}, peerResult
}

func serveHandshake(conn net.Conn, infoHash [sha1.Size]byte) error {
	request := make([]byte, 68)
	if _, err := io.ReadFull(conn, request); err != nil {
		return fmt.Errorf("read handshake: %w", err)
	}
	response := testHandshake(infoHash, [sha1.Size]byte{1, 2, 3})
	if err := writeFull(conn, response); err != nil {
		return fmt.Errorf("write handshake: %w", err)
	}
	return nil
}

func readPieceRequest(conn net.Conn, wantIndex int) (int, int, error) {
	request, err := readMessage(conn)
	if err != nil {
		return 0, 0, fmt.Errorf("read request: %w", err)
	}
	if request == nil || request.ID != 6 || len(request.Payload) != 12 {
		return 0, 0, fmt.Errorf("request message = %+v, want ID 6 with 12-byte payload", request)
	}
	index := int(binary.BigEndian.Uint32(request.Payload[0:4]))
	if index != wantIndex {
		return 0, 0, fmt.Errorf("request piece index = %d, want %d", index, wantIndex)
	}
	return int(binary.BigEndian.Uint32(request.Payload[4:8])), int(binary.BigEndian.Uint32(request.Payload[8:12])), nil
}

func writePieceBlock(conn net.Conn, index, begin int, block []byte) error {
	payload := make([]byte, 8+len(block))
	binary.BigEndian.PutUint32(payload[0:4], uint32(index))
	binary.BigEndian.PutUint32(payload[4:8], uint32(begin))
	copy(payload[8:], block)
	if err := writeMessage(conn, 7, payload); err != nil {
		return fmt.Errorf("write piece block at %d: %w", begin, err)
	}
	return nil
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

func TestClientHandshakeCancellationInterruptsPeerIO(t *testing.T) {
	clientConn, peerConn := net.Pipe()
	t.Cleanup(func() {
		clientConn.Close()
		peerConn.Close()
	})
	client := &Client{
		dialContext: func(context.Context, string, string) (net.Conn, error) {
			return clientConn, nil
		},
		random: bytes.NewReader(make([]byte, sha1.Size)),
	}

	requestRead := make(chan error, 1)
	go func() {
		_, err := io.ReadFull(peerConn, make([]byte, 68))
		requestRead <- err
	}()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := client.Handshake(ctx, "192.0.2.10:6881", [sha1.Size]byte{})
		result <- err
	}()
	if err := <-requestRead; err != nil {
		t.Fatalf("read handshake request: %v", err)
	}
	cancel()

	select {
	case err := <-result:
		if err == nil {
			t.Fatal("Handshake() error = nil after context cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("Handshake() did not stop after context cancellation")
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
