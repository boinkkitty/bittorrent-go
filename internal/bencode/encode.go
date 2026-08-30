package bencode

import (
	"fmt"
	"sort"
	"strconv"
)

// Encode serializes a supported value using canonical bencode ordering.
func Encode(val any) ([]byte, error) {
	return encodeValue(val)
}

func encodeValue(val any) ([]byte, error) {
	switch v := val.(type) {
	case string:
		return encodeString(v), nil
	case int:
		return encodeInteger(int64(v)), nil
	case int64:
		return encodeInteger(v), nil
	case []any:
		return encodeList(v)
	case map[string]any:
		return encodeMap(v)
	default:
		return nil, fmt.Errorf("unsupported type: %T", val)
	}
}

func encodeString(val string) []byte {
	result := strconv.AppendInt(nil, int64(len(val)), 10)
	result = append(result, ':')
	result = append(result, val...)

	return result
}

func encodeInteger(val int64) []byte {
	result := []byte{'i'}
	result = strconv.AppendInt(result, val, 10)
	result = append(result, 'e')

	return result
}

func encodeList(val []any) ([]byte, error) {
	result := []byte{'l'}

	for _, v := range val {
		encoded, err := encodeValue(v)
		if err != nil {
			return nil, err
		}

		result = append(result, encoded...)
	}

	result = append(result, 'e')
	return result, nil
}

func encodeMap(val map[string]any) ([]byte, error) {
	result := []byte{'d'}

	keys := make([]string, 0, len(val))
	for key := range val {
		keys = append(keys, key)
	}

	// Bencode dictionaries require lexicographically sorted keys.
	sort.Strings(keys)

	for _, key := range keys {
		result = append(result, encodeString(key)...)

		encodedValue, err := encodeValue(val[key])
		if err != nil {
			return nil, err
		}

		result = append(result, encodedValue...)
	}

	result = append(result, 'e')
	return result, nil
}
