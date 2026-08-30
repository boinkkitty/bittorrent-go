package peer

import (
	"bytes"
	"encoding/binary"
	"io"
	"strings"
	"testing"
)

func TestReadMessageAccumulatesFragmentedInput(t *testing.T) {
	wire := []byte{0, 0, 0, 4, 7, 0xaa, 0xbb, 0xcc}

	message, err := readMessage(&fragmentReader{data: wire, size: 2})
	if err != nil {
		t.Fatalf("readMessage() error = %v", err)
	}
	if message == nil {
		t.Fatal("readMessage() = nil, want a message")
	}
	if message.ID != 7 {
		t.Fatalf("readMessage() ID = %d, want 7", message.ID)
	}
	if !bytes.Equal(message.Payload, []byte{0xaa, 0xbb, 0xcc}) {
		t.Fatalf("readMessage() payload = %x, want aabbcc", message.Payload)
	}
}

func TestReadMessageReturnsNilForKeepAlive(t *testing.T) {
	message, err := readMessage(bytes.NewReader([]byte{0, 0, 0, 0}))
	if err != nil {
		t.Fatalf("readMessage() error = %v", err)
	}
	if message != nil {
		t.Fatalf("readMessage() = %+v, want nil keep-alive", message)
	}
}

func TestReadMessageRejectsOversizedLength(t *testing.T) {
	prefix := make([]byte, 4)
	binary.BigEndian.PutUint32(prefix, maxMessageSize+1)

	_, err := readMessage(bytes.NewReader(prefix))
	if err == nil {
		t.Fatal("readMessage() error = nil, want an error")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Fatalf("readMessage() error = %q, want it to contain %q", err, "too large")
	}
}

func TestWriteMessageEncodesLengthIDAndPayload(t *testing.T) {
	var wire bytes.Buffer

	if err := writeMessage(&wire, 6, []byte{0x01, 0x02}); err != nil {
		t.Fatalf("writeMessage() error = %v", err)
	}
	if got, want := wire.Bytes(), []byte{0, 0, 0, 3, 6, 1, 2}; !bytes.Equal(got, want) {
		t.Fatalf("writeMessage() = %x, want %x", got, want)
	}
}

type fragmentReader struct {
	data []byte
	size int
}

func (r *fragmentReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := min(len(p), r.size, len(r.data))
	copy(p, r.data[:n])
	r.data = r.data[n:]
	return n, nil
}
