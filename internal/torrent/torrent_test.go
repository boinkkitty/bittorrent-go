package torrent

import (
	"bytes"
	"crypto/sha1"
	"testing"
)

func TestParseSingleFileTorrent(t *testing.T) {
	info := []byte(
		"d6:lengthi92063e" +
			"4:name4:test" +
			"12:piece lengthi16384e" +
			"6:pieces20:",
	)
	info = append(info, bytes.Repeat([]byte{0xff}, 20)...)
	info = append(info, 'e')

	data := append([]byte("d8:announce14:http://tracker4:info"), info...)
	data = append(data, 'e')

	got, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse() returned error: %v", err)
	}

	want := Metadata{
		TrackerURL: "http://tracker",
		Length:     92063,
		Hash:       sha1.Sum(info),
	}
	if got != want {
		t.Fatalf("Parse() = %#v, want %#v", got, want)
	}
}

func TestParseRejectsInvalidTorrentMetadata(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "malformed bencode", data: "not bencode"},
		{name: "top level is not dictionary", data: "le"},
		{name: "missing announce", data: "d4:infod6:lengthi1eee"},
		{name: "announce has wrong type", data: "d8:announcei1e4:infod6:lengthi1eeee"},
		{name: "missing info", data: "d8:announce14:http://trackere"},
		{name: "info has wrong type", data: "d8:announce14:http://tracker4:info4:nopee"},
		{name: "missing length", data: "d8:announce14:http://tracker4:infodee"},
		{name: "length has wrong type", data: "d8:announce14:http://tracker4:infod6:length4:nopeee"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Parse([]byte(tt.data)); err == nil {
				t.Fatalf("Parse(%q) returned no error", tt.data)
			}
		})
	}
}
