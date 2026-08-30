package tracker

import (
	"context"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/boinkkitty/bittorrent-go/internal/bencode"
	"github.com/boinkkitty/bittorrent-go/internal/torrent"
)

func TestNewClientUsesTrackerDefaults(t *testing.T) {
	client := NewClient(nil)

	if client.httpClient != http.DefaultClient {
		t.Fatalf("NewClient(nil) HTTP client = %p, want http.DefaultClient", client.httpClient)
	}
	wantPeerID := [sha1.Size]byte([]byte("-BG0001-123456789012"))
	if client.peerID != wantPeerID {
		t.Fatalf("NewClient(nil) peer ID = %q, want %q", client.peerID, wantPeerID)
	}
	if client.port != 6881 {
		t.Fatalf("NewClient(nil) port = %d, want 6881", client.port)
	}
}

func TestNewClientUsesProvidedHTTPClient(t *testing.T) {
	httpClient := &http.Client{}

	client := NewClient(httpClient)

	if client.httpClient != httpClient {
		t.Fatalf("NewClient(httpClient) HTTP client = %p, want %p", client.httpClient, httpClient)
	}
}

func TestPeerStoresIPv4AddressAndPort(t *testing.T) {
	wantIP := netip.AddrFrom4([4]byte{165, 232, 41, 73})
	peer := Peer{IP: wantIP, Port: 51556}

	if peer.IP != wantIP {
		t.Fatalf("Peer.IP = %s, want %s", peer.IP, wantIP)
	}
	if peer.Port != 51556 {
		t.Fatalf("Peer.Port = %d, want 51556", peer.Port)
	}
}

func TestClientPeersSendsAnnounceRequestAndParsesCompactPeers(t *testing.T) {
	infoHash := [sha1.Size]byte{
		0x00, 0x20, 0x2b, 0x26, 0x25, 0x2f, 0x3f, 0x7f, 0x80, 0xff,
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a,
	}
	compactPeers := make([]byte, 12)
	copy(compactPeers[0:4], []byte{165, 232, 41, 73})
	binary.BigEndian.PutUint16(compactPeers[4:6], 51556)
	copy(compactPeers[6:10], []byte{165, 232, 38, 164})
	binary.BigEndian.PutUint16(compactPeers[10:12], 51493)

	responseBody, err := bencode.Encode(map[string]any{
		"interval": 900,
		"peers":    string(compactPeers),
	})
	if err != nil {
		t.Fatalf("encode tracker response: %v", err)
	}

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("request method = %q, want GET", r.Method)
		}
		if r.URL.Path != "/announce" {
			t.Errorf("request path = %q, want /announce", r.URL.Path)
		}

		query := r.URL.Query()
		if got := []byte(query.Get("info_hash")); string(got) != string(infoHash[:]) {
			t.Errorf("info_hash bytes = %x, want %x", got, infoHash)
		}
		if got := query.Get("info_hash"); got == hex.EncodeToString(infoHash[:]) {
			t.Errorf("info_hash = hexadecimal representation %q, want raw bytes", got)
		}
		if got := []byte(query.Get("peer_id")); string(got) != string(defaultPeerID[:]) {
			t.Errorf("peer_id bytes = %q, want %q", got, defaultPeerID)
		}
		for key, want := range map[string]string{
			"token":      "keep-me",
			"port":       "6881",
			"uploaded":   "0",
			"downloaded": "0",
			"left":       "92063",
			"compact":    "1",
		} {
			if got := query.Get(key); got != want {
				t.Errorf("query parameter %q = %q, want %q", key, got, want)
			}
		}

		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(responseBody); err != nil {
			t.Errorf("write tracker response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	metadata := torrent.Metadata{
		TrackerURL: server.URL + "/announce?token=keep-me",
		Length:     92063,
		Hash:       infoHash,
	}
	peers, err := NewClient(server.Client()).Peers(context.Background(), metadata)
	if err != nil {
		t.Fatalf("Peers() error = %v", err)
	}

	wantPeers := []Peer{
		{IP: netip.AddrFrom4([4]byte{165, 232, 41, 73}), Port: 51556},
		{IP: netip.AddrFrom4([4]byte{165, 232, 38, 164}), Port: 51493},
	}
	if len(peers) != len(wantPeers) {
		t.Fatalf("Peers() returned %d peers, want %d", len(peers), len(wantPeers))
	}
	for i := range wantPeers {
		if peers[i] != wantPeers[i] {
			t.Errorf("Peers()[%d] = %+v, want %+v", i, peers[i], wantPeers[i])
		}
	}
}

func TestClientPeersRejectsInvalidTrackerResponses(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantError  string
	}{
		{
			name:       "HTTP failure",
			statusCode: http.StatusInternalServerError,
			body:       "tracker unavailable",
			wantError:  "500 Internal Server Error",
		},
		{
			name:       "invalid bencode",
			statusCode: http.StatusOK,
			body:       "not bencode",
			wantError:  "decode tracker response",
		},
		{
			name:       "non-dictionary response",
			statusCode: http.StatusOK,
			body:       "le",
			wantError:  "must be a dictionary",
		},
		{
			name:       "tracker failure reason",
			statusCode: http.StatusOK,
			body:       "d14:failure reason25:torrent is not registerede",
			wantError:  "torrent is not registered",
		},
		{
			name:       "missing peers",
			statusCode: http.StatusOK,
			body:       "d8:intervali900ee",
			wantError:  "invalid peers field",
		},
		{
			name:       "incomplete compact peer",
			statusCode: http.StatusOK,
			body:       "d5:peers5:abcdee",
			wantError:  "divisible by 6",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.statusCode)
				if _, err := w.Write([]byte(tt.body)); err != nil {
					t.Errorf("write tracker response: %v", err)
				}
			}))
			t.Cleanup(server.Close)

			metadata := torrent.Metadata{TrackerURL: server.URL, Length: 1}
			_, err := NewClient(server.Client()).Peers(context.Background(), metadata)
			if err == nil {
				t.Fatal("Peers() error = nil, want an error")
			}
			if !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("Peers() error = %q, want it to contain %q", err, tt.wantError)
			}
		})
	}
}
