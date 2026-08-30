package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
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
