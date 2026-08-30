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
