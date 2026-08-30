package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/boinkkitty/bittorrent-go/internal/bencode"
	"github.com/boinkkitty/bittorrent-go/internal/torrent"
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
		return 0

	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", args[0])
		return 1
	}
}
