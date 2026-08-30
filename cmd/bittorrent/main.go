package main

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"net/netip"
	"os"
	"strconv"

	"github.com/boinkkitty/bittorrent-go/internal/bencode"
	"github.com/boinkkitty/bittorrent-go/internal/peer"
	"github.com/boinkkitty/bittorrent-go/internal/torrent"
	"github.com/boinkkitty/bittorrent-go/internal/tracker"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: bittorrent <command> [arguments]")
		return 1
	}

	switch args[0] {
	case "decode":
		if len(args) != 2 {
			fmt.Fprintln(stderr, "usage: bittorrent decode <bencoded-value>")
			return 1
		}

		decoded, err := bencode.Decode(args[1])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}

		jsonOutput, err := json.Marshal(decoded)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintln(stdout, string(jsonOutput))
		return 0

	case "info":
		if len(args) != 2 {
			fmt.Fprintln(stderr, "usage: bittorrent info <torrent-file>")
			return 1
		}

		data, err := os.ReadFile(args[1])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}

		metadata, err := torrent.Parse(data)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}

		fmt.Fprintf(stdout, "Tracker URL: %s\n", metadata.TrackerURL)
		fmt.Fprintf(stdout, "Length: %d\n", metadata.Length)
		fmt.Fprintf(stdout, "Info Hash: %x\n", metadata.Hash)
		fmt.Fprintf(stdout, "Piece Length: %d\n", metadata.PieceLength)
		fmt.Fprintln(stdout, "Piece Hashes:")
		for _, pieceHash := range metadata.PieceHashes {
			fmt.Fprintf(stdout, "%x\n", pieceHash)
		}
		return 0

	case "peers":
		if len(args) != 2 {
			fmt.Fprintln(stderr, "usage: bittorrent peers <torrent-file>")
			return 1
		}

		data, err := os.ReadFile(args[1])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}

		metadata, err := torrent.Parse(data)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}

		peers, err := tracker.NewClient(nil).Peers(context.Background(), metadata)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		for _, peer := range peers {
			fmt.Fprintf(stdout, "%s:%d\n", peer.IP, peer.Port)
		}
		return 0

	case "handshake":
		if len(args) != 3 {
			fmt.Fprintln(stderr, "usage: bittorrent handshake <torrent-file> <peer-address>")
			return 1
		}

		data, err := os.ReadFile(args[1])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}

		metadata, err := torrent.Parse(data)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}

		remotePeerID, err := peer.NewClient().Handshake(context.Background(), args[2], metadata.Hash)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "Peer ID: %x\n", remotePeerID)
		return 0

	case "download_piece":
		if len(args) != 5 || args[1] != "-o" {
			fmt.Fprintln(stderr, "usage: bittorrent download_piece -o <output-path> <torrent-file> <piece-index>")
			return 1
		}

		pieceIndex, err := strconv.Atoi(args[4])
		if err != nil {
			fmt.Fprintf(stderr, "invalid piece index %q: %v\n", args[4], err)
			return 1
		}
		data, err := os.ReadFile(args[3])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		metadata, err := torrent.Parse(data)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		pieceLength, err := metadata.PieceSize(pieceIndex)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}

		var sessionPeerID [sha1.Size]byte
		if _, err := io.ReadFull(rand.Reader, sessionPeerID[:]); err != nil {
			fmt.Fprintf(stderr, "generate peer ID: %v\n", err)
			return 1
		}
		availablePeers, err := tracker.NewClientWithPeerID(nil, sessionPeerID).Peers(context.Background(), metadata)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if len(availablePeers) == 0 {
			fmt.Fprintln(stderr, "tracker returned no peers")
			return 1
		}

		peerClient := peer.NewClientWithPeerID(sessionPeerID)
		var pieceData []byte
		var lastPeerError error
		for _, availablePeer := range availablePeers {
			address := netip.AddrPortFrom(availablePeer.IP, availablePeer.Port).String()
			pieceData, lastPeerError = peerClient.DownloadPiece(
				context.Background(),
				address,
				metadata.Hash,
				pieceIndex,
				pieceLength,
				metadata.PieceHashes[pieceIndex],
			)
			if lastPeerError == nil {
				break
			}
		}
		if lastPeerError != nil {
			fmt.Fprintf(stderr, "download piece %d: all peers failed: %v\n", pieceIndex, lastPeerError)
			return 1
		}
		if err := os.WriteFile(args[2], pieceData, 0o644); err != nil {
			fmt.Fprintf(stderr, "write piece to %q: %v\n", args[2], err)
			return 1
		}
		fmt.Fprintf(stdout, "Piece %d downloaded to %s.\n", pieceIndex, args[2])
		return 0

	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", args[0])
		return 1
	}
}
