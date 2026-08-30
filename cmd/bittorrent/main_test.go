package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"fmt"
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
