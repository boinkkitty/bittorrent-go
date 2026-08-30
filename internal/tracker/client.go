package tracker

import (
	"context"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"

	"github.com/boinkkitty/bittorrent-go/internal/bencode"
	"github.com/boinkkitty/bittorrent-go/internal/torrent"
)

const defaultPort uint16 = 6881

var defaultPeerID = [sha1.Size]byte([]byte("-BG0001-123456789012"))

type Peer struct {
	IP   netip.Addr
	Port uint16
}

type Client struct {
	httpClient *http.Client
	peerID     [sha1.Size]byte
	port       uint16
}

func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	return &Client{
		httpClient: httpClient,
		peerID:     defaultPeerID,
		port:       defaultPort,
	}
}

func (c *Client) Peers(ctx context.Context, metadata torrent.Metadata) ([]Peer, error) {
	trackerURL, err := url.Parse(metadata.TrackerURL)
	if err != nil {
		return nil, fmt.Errorf("parse tracker URL: %w", err)
	}

	trackerURL.RawQuery = c.buildPeerQuery(metadata, trackerURL.Query())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, trackerURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create tracker request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request peers from tracker: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("tracker returned %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read tracker response: %w", err)
	}

	decoded, err := bencode.Decode(string(body))
	if err != nil {
		return nil, fmt.Errorf("decode tracker response: %w", err)
	}
	response, ok := decoded.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("tracker response must be a dictionary")
	}
	if reason, ok := response["failure reason"].(string); ok {
		return nil, fmt.Errorf("tracker failure: %s", reason)
	}
	compactPeers, ok := response["peers"].(string)
	if !ok {
		return nil, fmt.Errorf("tracker response has invalid peers field")
	}
	if len(compactPeers)%6 != 0 {
		return nil, fmt.Errorf("compact peers length must be divisible by 6")
	}

	peers := make([]Peer, len(compactPeers)/6)
	for i := range peers {
		offset := i * 6
		var address [4]byte
		copy(address[:], compactPeers[offset:offset+4])
		peers[i] = Peer{
			IP:   netip.AddrFrom4(address),
			Port: binary.BigEndian.Uint16([]byte(compactPeers[offset+4 : offset+6])),
		}
	}
	return peers, nil
}

func (c *Client) buildPeerQuery(metadata torrent.Metadata, query url.Values) string {
	query.Set("info_hash", string(metadata.Hash[:]))
	query.Set("peer_id", string(c.peerID[:]))
	query.Set("port", strconv.FormatUint(uint64(c.port), 10))
	query.Set("uploaded", "0")
	query.Set("downloaded", "0")
	query.Set("left", strconv.Itoa(metadata.Length))
	query.Set("compact", "1")
	return query.Encode()
}
