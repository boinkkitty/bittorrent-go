package main

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestRunInfoPrintsTrackerURLAndLength(t *testing.T) {
	torrentPath := filepath.Join(t.TempDir(), "sample.torrent")
	data := []byte("d8:announce14:http://tracker4:infod6:lengthi92063eee")
	if err := os.WriteFile(torrentPath, data, 0o600); err != nil {
		t.Fatalf("write torrent fixture: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{"info", torrentPath}, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("run() exit code = %d, want 0; stderr: %s", exitCode, stderr.String())
	}
	infoHash := sha1.Sum([]byte("d6:lengthi92063ee"))
	wantOutput := fmt.Sprintf(
		"Tracker URL: http://tracker\nLength: 92063\nInfo Hash: %x\n",
		infoHash,
	)
	if got := stdout.String(); got != wantOutput {
		t.Fatalf("run() stdout = %q, want %q", got, wantOutput)
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("run() stderr = %q, want empty output", got)
	}
}
