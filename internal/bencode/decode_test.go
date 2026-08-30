package bencode

import (
	"reflect"
	"testing"
)

func TestDecodeBencodeScalars(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  any
	}{
		{name: "string", input: "5:hello", want: "hello"},
		{name: "positive integer", input: "i52e", want: 52},
		{name: "negative integer", input: "i-52e", want: -52},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Decode(tt.input)
			if err != nil {
				t.Fatalf("Decode(%q) returned error: %v", tt.input, err)
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Decode(%q) = %#v, want %#v", tt.input, got, tt.want)
			}
		})
	}
}

func TestDecodeBencodeList(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  any
	}{
		{
			name:  "string and integer",
			input: "l5:helloi52ee",
			want:  []any{"hello", 52},
		},
		{
			name:  "empty list",
			input: "le",
			want:  []any{},
		},
		{
			name:  "nested list",
			input: "ll3:fooei-7ee",
			want:  []any{[]any{"foo"}, -7},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Decode(tt.input)
			if err != nil {
				t.Fatalf("Decode(%q) returned error: %v", tt.input, err)
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Decode(%q) = %#v, want %#v", tt.input, got, tt.want)
			}
		})
	}
}

func TestDecodeBencodeDictionary(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  any
	}{
		{
			name:  "string and integer values",
			input: "d3:foo3:bar5:helloi52ee",
			want:  map[string]any{"foo": "bar", "hello": 52},
		},
		{
			name:  "empty dictionary",
			input: "de",
			want:  map[string]any{},
		},
		{
			name:  "nested list value",
			input: "d4:listli1ei2eee",
			want:  map[string]any{"list": []any{1, 2}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Decode(tt.input)
			if err != nil {
				t.Fatalf("Decode(%q) returned error: %v", tt.input, err)
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Decode(%q) = %#v, want %#v", tt.input, got, tt.want)
			}
		})
	}
}

func TestDecodeBencodeRejectsInvalidDictionaryKeys(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "non-string key", input: "di1e3:fooe"},
		{name: "keys out of order", input: "d1:bi1e1:ai2ee"},
		{name: "duplicate key", input: "d1:ai1e1:ai2ee"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Decode(tt.input); err == nil {
				t.Fatalf("Decode(%q) returned no error", tt.input)
			}
		})
	}
}
