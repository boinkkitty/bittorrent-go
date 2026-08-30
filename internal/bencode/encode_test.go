package bencode

import (
	"bytes"
	"testing"
)

func TestEncode(t *testing.T) {
	binaryString := string([]byte{0xff, 0x00})

	tests := []struct {
		name  string
		value any
		want  []byte
	}{
		{name: "string", value: "spam", want: []byte("4:spam")},
		{name: "integer", value: 42, want: []byte("i42e")},
		{name: "list", value: []any{"spam", 42}, want: []byte("l4:spami42ee")},
		{
			name:  "sorted dictionary with binary string",
			value: map[string]any{"pieces": binaryString, "length": 3},
			want:  append([]byte("d6:lengthi3e6:pieces2:"), 0xff, 0x00, 'e'),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Encode(tt.value)
			if err != nil {
				t.Fatalf("Encode() returned error: %v", err)
			}
			if !bytes.Equal(got, tt.want) {
				t.Fatalf("Encode() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEncodeRejectsUnsupportedType(t *testing.T) {
	if _, err := Encode(true); err == nil {
		t.Fatal("Encode(true) returned no error")
	}
}
