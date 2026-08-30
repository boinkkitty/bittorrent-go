package torrent

import (
	"crypto/sha1"
	"reflect"
	"testing"
)

func TestParseSingleFileTorrent(t *testing.T) {
	firstPieceHash := [sha1.Size]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19}
	secondPieceHash := [sha1.Size]byte{20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39}

	info := []byte(
		"d6:lengthi92063e" +
			"4:name4:test" +
			"12:piece lengthi16384e" +
			"6:pieces40:",
	)
	info = append(info, firstPieceHash[:]...)
	info = append(info, secondPieceHash[:]...)
	info = append(info, 'e')

	data := append([]byte("d8:announce14:http://tracker4:info"), info...)
	data = append(data, 'e')

	got, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse() returned error: %v", err)
	}

	want := Metadata{
		TrackerURL:  "http://tracker",
		Length:      92063,
		Hash:        sha1.Sum(info),
		PieceLength: 16384,
		PieceHashes: [][sha1.Size]byte{firstPieceHash, secondPieceHash},
	}
	if !reflect.DeepEqual(got, want) {
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
		{name: "missing piece length", data: "d8:announce14:http://tracker4:infod6:lengthi1e6:pieces0:ee"},
		{name: "piece length has wrong type", data: "d8:announce14:http://tracker4:infod6:lengthi1e12:piece length1:x6:pieces0:ee"},
		{name: "missing pieces", data: "d8:announce14:http://tracker4:infod6:lengthi1e12:piece lengthi1eee"},
		{name: "pieces has wrong type", data: "d8:announce14:http://tracker4:infod6:lengthi1e12:piece lengthi1e6:piecesi1eee"},
		{name: "pieces length is not divisible by SHA-1 size", data: "d8:announce14:http://tracker4:infod6:lengthi1e12:piece lengthi1e6:pieces1:xee"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Parse([]byte(tt.data)); err == nil {
				t.Fatalf("Parse(%q) returned no error", tt.data)
			}
		})
	}
}
