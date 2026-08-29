package bencode

import (
	"fmt"
	"strconv"
)

type decoder struct {
	input    string
	position int
}

func Decode(input string) (any, error) {
	d := decoder{input: input}

	value, err := d.decodeValue()
	if err != nil {
		return nil, err
	}
	if d.position != len(input) {
		return nil, fmt.Errorf("unexpected data after bencoded value")
	}

	return value, nil
}

func (d *decoder) decodeValue() (any, error) {
	if d.position >= len(d.input) {
		return nil, fmt.Errorf("unexpected end of bencoded value")
	}

	switch d.input[d.position] {
	case 'i':
		return d.decodeInteger()
	case 'l':
		return d.decodeList()
	case 'd':
		return d.decodeDictionary()
	default:
		return d.decodeString()
	}
}

func (d *decoder) decodeInteger() (int, error) {
	start := d.position + 1
	end := start
	for end < len(d.input) && d.input[end] != 'e' {
		end++
	}
	if end == len(d.input) {
		return 0, fmt.Errorf("unterminated bencoded integer")
	}

	value, err := strconv.Atoi(d.input[start:end])
	if err != nil {
		return 0, fmt.Errorf("invalid bencoded integer: %w", err)
	}

	d.position = end + 1
	return value, nil
}

func (d *decoder) decodeString() (string, error) {
	start := d.position
	colon := start
	for colon < len(d.input) && d.input[colon] != ':' {
		if d.input[colon] < '0' || d.input[colon] > '9' {
			return "", fmt.Errorf("invalid bencoded string length")
		}
		colon++
	}
	if colon == len(d.input) || colon == start {
		return "", fmt.Errorf("invalid bencoded string")
	}

	length, err := strconv.Atoi(d.input[start:colon])
	if err != nil {
		return "", fmt.Errorf("invalid bencoded string length: %w", err)
	}

	valueStart := colon + 1
	valueEnd := valueStart + length
	if valueEnd > len(d.input) {
		return "", fmt.Errorf("bencoded string is shorter than its declared length")
	}

	d.position = valueEnd
	return d.input[valueStart:valueEnd], nil
}

func (d *decoder) decodeList() ([]any, error) {
	d.position++
	values := make([]any, 0)

	for {
		if d.position >= len(d.input) {
			return nil, fmt.Errorf("unterminated bencoded list")
		}
		if d.input[d.position] == 'e' {
			d.position++
			return values, nil
		}

		value, err := d.decodeValue()
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
}

func (d *decoder) decodeDictionary() (map[string]any, error) {
	d.position++
	values := make(map[string]any)
	previousKey := ""
	hasPreviousKey := false

	for {
		if d.position >= len(d.input) {
			return nil, fmt.Errorf("unterminated bencoded dictionary")
		}
		if d.input[d.position] == 'e' {
			d.position++
			return values, nil
		}

		key, err := d.decodeString()
		if err != nil {
			return nil, fmt.Errorf("invalid dictionary key: %w", err)
		}
		if hasPreviousKey && key <= previousKey {
			return nil, fmt.Errorf("dictionary keys must be unique and lexicographically sorted")
		}
		previousKey = key
		hasPreviousKey = true

		value, err := d.decodeValue()
		if err != nil {
			return nil, fmt.Errorf("invalid value for dictionary key %q: %w", key, err)
		}
		values[key] = value
	}
}
