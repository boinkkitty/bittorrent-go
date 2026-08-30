package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/boinkkitty/bittorrent-go/internal/bencode"
)

func TestRunInfoPrintsTorrentMetadata(t *testing.T) {
	torrentPath := filepath.Join(t.TempDir(), "sample.torrent")
	firstPieceHash := bytes.Repeat([]byte{0x11}, sha1.Size)
	secondPieceHash := bytes.Repeat([]byte{0x22}, sha1.Size)
	info := []byte("d6:lengthi92063e12:piece lengthi32768e6:pieces40:")
	info = append(info, firstPieceHash...)
	info = append(info, secondPieceHash...)
	info = append(info, 'e')
	data := append([]byte("d8:announce14:http://tracker4:info"), info...)
	data = append(data, 'e')
	if err := os.WriteFile(torrentPath, data, 0o600); err != nil {
		t.Fatalf("write torrent fixture: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"info", torrentPath}, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("run() exit code = %d, want 0; stderr: %s", exitCode, stderr.String())
	}
	infoHash := sha1.Sum(info)
	wantOutput := fmt.Sprintf(
		"Tracker URL: http://tracker\n"+
			"Length: 92063\n"+
			"Info Hash: %x\n"+
			"Piece Length: 32768\n"+
			"Piece Hashes:\n%s\n%s\n",
		infoHash,
		hex.EncodeToString(firstPieceHash),
		hex.EncodeToString(secondPieceHash),
	)
	if got := stdout.String(); got != wantOutput {
		t.Fatalf("run() stdout = %q, want %q", got, wantOutput)
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("run() stderr = %q, want empty output", got)
	}
}

func TestRunHandshakePrintsRemotePeerID(t *testing.T) {
	torrentData, err := bencode.Encode(map[string]any{
		"announce": "http://tracker",
		"info": map[string]any{
			"length":       92063,
			"piece length": 32768,
			"pieces":       string(bytes.Repeat([]byte{0x11}, sha1.Size)),
		},
	})
	if err != nil {
		t.Fatalf("encode torrent fixture: %v", err)
	}
	torrentPath := filepath.Join(t.TempDir(), "sample.torrent")
	if err := os.WriteFile(torrentPath, torrentData, 0o600); err != nil {
		t.Fatalf("write torrent fixture: %v", err)
	}

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for peer connection: %v", err)
	}
	t.Cleanup(func() { listener.Close() })
	remotePeerID := [sha1.Size]byte{
		1, 2, 3, 4, 5, 6, 7, 8, 9, 10,
		11, 12, 13, 14, 15, 16, 17, 18, 19, 20,
	}
	peerResult := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			peerResult <- fmt.Errorf("accept connection: %w", err)
			return
		}
		defer conn.Close()

		request := make([]byte, 68)
		if _, err := io.ReadFull(conn, request); err != nil {
			peerResult <- fmt.Errorf("read handshake: %w", err)
			return
		}
		response := make([]byte, 68)
		response[0] = 19
		copy(response[1:20], "BitTorrent protocol")
		copy(response[28:48], request[28:48])
		copy(response[48:68], remotePeerID[:])
		if _, err := conn.Write(response); err != nil {
			peerResult <- fmt.Errorf("write handshake: %w", err)
			return
		}
		peerResult <- nil
	}()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"handshake", torrentPath, listener.Addr().String()}, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("run() exit code = %d, want 0; stderr: %s", exitCode, stderr.String())
	}
	if got, want := stdout.String(), "Peer ID: 0102030405060708090a0b0c0d0e0f1011121314\n"; got != want {
		t.Fatalf("run() stdout = %q, want %q", got, want)
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("run() stderr = %q, want empty output", got)
	}
	if err := <-peerResult; err != nil {
		t.Fatalf("peer exchange: %v", err)
	}
}

func TestRunHandshakeRequiresTorrentAndPeerAddress(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run([]string{"handshake", "sample.torrent"}, &stdout, &stderr)

	if exitCode != 1 {
		t.Fatalf("run() exit code = %d, want 1", exitCode)
	}
	if got, want := stderr.String(), "usage: bittorrent handshake <torrent-file> <peer-address>\n"; got != want {
		t.Fatalf("run() stderr = %q, want %q", got, want)
	}
	if got := stdout.String(); got != "" {
		t.Fatalf("run() stdout = %q, want empty output", got)
	}
}

func TestRunDownloadPieceFetchesVerifiesAndWritesPiece(t *testing.T) {
	pieceData := make([]byte, 16*1024+321)
	for i := range pieceData {
		pieceData[i] = byte(i % 251)
	}
	pieceHash := sha1.Sum(pieceData)

	peerListener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for peer connection: %v", err)
	}
	t.Cleanup(func() { peerListener.Close() })
	peerAddress := peerListener.Addr().(*net.TCPAddr)
	compactPeer := make([]byte, 6)
	copy(compactPeer[:4], peerAddress.IP.To4())
	binary.BigEndian.PutUint16(compactPeer[4:6], uint16(peerAddress.Port))
	trackerResponse, err := bencode.Encode(map[string]any{
		"interval": 900,
		"peers":    string(compactPeer),
	})
	if err != nil {
		t.Fatalf("encode tracker response: %v", err)
	}
	announcedPeerID := make(chan string, 1)
	trackerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		announcedPeerID <- r.URL.Query().Get("peer_id")
		if _, err := w.Write(trackerResponse); err != nil {
			t.Errorf("write tracker response: %v", err)
		}
	}))
	t.Cleanup(trackerServer.Close)

	torrentData, err := bencode.Encode(map[string]any{
		"announce": trackerServer.URL,
		"info": map[string]any{
			"length":       len(pieceData),
			"piece length": len(pieceData),
			"pieces":       string(pieceHash[:]),
		},
	})
	if err != nil {
		t.Fatalf("encode torrent fixture: %v", err)
	}
	torrentPath := filepath.Join(t.TempDir(), "sample.torrent")
	if err := os.WriteFile(torrentPath, torrentData, 0o600); err != nil {
		t.Fatalf("write torrent fixture: %v", err)
	}
	outputPath := filepath.Join(t.TempDir(), "piece-0")

	peerResult := make(chan error, 1)
	go func() {
		conn, err := peerListener.Accept()
		if err != nil {
			peerResult <- fmt.Errorf("accept peer connection: %w", err)
			return
		}
		defer conn.Close()

		handshake := make([]byte, 68)
		if _, err := io.ReadFull(conn, handshake); err != nil {
			peerResult <- fmt.Errorf("read handshake: %w", err)
			return
		}
		wantPeerID := <-announcedPeerID
		if got := string(handshake[48:68]); got != wantPeerID {
			peerResult <- fmt.Errorf("handshake peer ID %x does not match tracker peer ID %x", got, wantPeerID)
			return
		}
		response := append([]byte(nil), handshake...)
		copy(response[48:68], bytes.Repeat([]byte{0x42}, sha1.Size))
		if _, err := conn.Write(response); err != nil {
			peerResult <- fmt.Errorf("write handshake: %w", err)
			return
		}
		if err := writeTestPeerMessage(conn, 5, []byte{0xff}); err != nil {
			peerResult <- err
			return
		}
		id, payload, err := readTestPeerMessage(conn)
		if err != nil || id != 2 || len(payload) != 0 {
			peerResult <- fmt.Errorf("read interested: id=%d payload=%x err=%v", id, payload, err)
			return
		}
		if err := writeTestPeerMessage(conn, 1, nil); err != nil {
			peerResult <- err
			return
		}

		type blockRequest struct{ begin, length int }
		requests := make([]blockRequest, 0, 2)
		for range 2 {
			id, payload, err := readTestPeerMessage(conn)
			if err != nil || id != 6 || len(payload) != 12 {
				peerResult <- fmt.Errorf("read block request: id=%d payload=%x err=%v", id, payload, err)
				return
			}
			if index := binary.BigEndian.Uint32(payload[0:4]); index != 0 {
				peerResult <- fmt.Errorf("request piece index = %d, want 0", index)
				return
			}
			requests = append(requests, blockRequest{
				begin:  int(binary.BigEndian.Uint32(payload[4:8])),
				length: int(binary.BigEndian.Uint32(payload[8:12])),
			})
		}
		for i := len(requests) - 1; i >= 0; i-- {
			request := requests[i]
			payload := make([]byte, 8+request.length)
			binary.BigEndian.PutUint32(payload[0:4], 0)
			binary.BigEndian.PutUint32(payload[4:8], uint32(request.begin))
			copy(payload[8:], pieceData[request.begin:request.begin+request.length])
			if err := writeTestPeerMessage(conn, 7, payload); err != nil {
				peerResult <- err
				return
			}
		}
		peerResult <- nil
	}()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"download_piece", "-o", outputPath, torrentPath, "0"}, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("run() exit code = %d, want 0; stderr: %s", exitCode, stderr.String())
	}
	written, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read downloaded piece: %v", err)
	}
	if !bytes.Equal(written, pieceData) {
		t.Fatal("downloaded piece does not match expected data")
	}
	if got, want := stdout.String(), "Piece 0 downloaded to "+outputPath+".\n"; got != want {
		t.Fatalf("run() stdout = %q, want %q", got, want)
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("run() stderr = %q, want empty output", got)
	}
	if err := <-peerResult; err != nil {
		t.Fatalf("peer exchange: %v", err)
	}
}

func readTestPeerMessage(r io.Reader) (byte, []byte, error) {
	var prefix [4]byte
	if _, err := io.ReadFull(r, prefix[:]); err != nil {
		return 0, nil, err
	}
	length := int(binary.BigEndian.Uint32(prefix[:]))
	if length < 1 {
		return 0, nil, fmt.Errorf("invalid message length %d", length)
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return 0, nil, err
	}
	return body[0], body[1:], nil
}

func writeTestPeerMessage(w io.Writer, id byte, payload []byte) error {
	wire := make([]byte, 5+len(payload))
	binary.BigEndian.PutUint32(wire[:4], uint32(1+len(payload)))
	wire[4] = id
	copy(wire[5:], payload)
	_, err := w.Write(wire)
	return err
}

func TestRunPeersPrintsCompactTrackerPeers(t *testing.T) {
	compactPeers := make([]byte, 12)
	copy(compactPeers[0:4], []byte{165, 232, 41, 73})
	binary.BigEndian.PutUint16(compactPeers[4:6], 51556)
	copy(compactPeers[6:10], []byte{165, 232, 38, 164})
	binary.BigEndian.PutUint16(compactPeers[10:12], 51493)
	trackerResponse, err := bencode.Encode(map[string]any{
		"interval": 900,
		"peers":    string(compactPeers),
	})
	if err != nil {
		t.Fatalf("encode tracker response: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write(trackerResponse); err != nil {
			t.Errorf("write tracker response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	torrentData, err := bencode.Encode(map[string]any{
		"announce": server.URL,
		"info": map[string]any{
			"length":       92063,
			"piece length": 32768,
			"pieces":       string(bytes.Repeat([]byte{0x11}, sha1.Size)),
		},
	})
	if err != nil {
		t.Fatalf("encode torrent fixture: %v", err)
	}
	torrentPath := filepath.Join(t.TempDir(), "sample.torrent")
	if err := os.WriteFile(torrentPath, torrentData, 0o600); err != nil {
		t.Fatalf("write torrent fixture: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"peers", torrentPath}, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("run() exit code = %d, want 0; stderr: %s", exitCode, stderr.String())
	}
	if got, want := stdout.String(), "165.232.41.73:51556\n165.232.38.164:51493\n"; got != want {
		t.Fatalf("run() stdout = %q, want %q", got, want)
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("run() stderr = %q, want empty output", got)
	}
}
