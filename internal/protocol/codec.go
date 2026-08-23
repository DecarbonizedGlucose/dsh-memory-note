package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

const (
	MaxRequestSize = 96 * 1024

	maxMetadataDepth = 32

	// maxJSONDepth caps total nesting so the recursive checker cannot overflow;
	// it must exceed maxMetadataDepth.
	maxJSONDepth = 64
)

func Decode(raw string, request any) error {
	if raw == "" {
		return Invalid("request JSON is required")
	}
	if len(raw) > MaxRequestSize {
		return Invalid("request JSON exceeds 96 KiB")
	}
	if !utf8.ValidString(raw) {
		return Invalid("request JSON must be valid UTF-8")
	}
	if err := inspectJSON(raw); err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(request); err != nil {
		return Invalid("request fields are invalid")
	}
	if token, err := decoder.Token(); !errors.Is(err, io.EOF) || token != nil {
		return Invalid("request must contain one JSON object")
	}
	return nil
}

func inspectJSON(raw string) error {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil {
		return Invalid("request is not valid JSON")
	}
	if token != json.Delim('{') {
		return Invalid("request must be a JSON object")
	}
	if err := inspectObject(decoder, ""); err != nil {
		return err
	}
	if token, err := decoder.Token(); !errors.Is(err, io.EOF) || token != nil {
		return Invalid("request must contain one JSON object")
	}
	return nil
}

func inspectObject(decoder *json.Decoder, path string) error {
	if strings.Count(path, "/") > maxJSONDepth {
		return Invalid("request is nested too deeply")
	}
	keys := make(map[string]struct{})
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return Invalid("request contains an invalid object")
		}
		key, ok := token.(string)
		if !ok {
			return Invalid("request contains an invalid object key")
		}
		if _, exists := keys[key]; exists {
			return Invalid("request contains a duplicate field")
		}
		keys[key] = struct{}{}
		if !cleanControl(key) {
			return Invalid("request contains a control character")
		}
		valuePath := path + "/" + key
		if metadataDepth(valuePath) > maxMetadataDepth {
			return Invalid("metadata is nested too deeply")
		}
		if err := inspectValue(decoder, valuePath); err != nil {
			return err
		}
	}
	if _, err := decoder.Token(); err != nil {
		return Invalid("request contains an invalid object")
	}
	return nil
}

func inspectValue(decoder *json.Decoder, path string) error {
	token, err := decoder.Token()
	if err != nil {
		return Invalid("request contains an invalid value")
	}
	if token == nil && !insideMetadata(path) {
		return Invalid("null is not allowed")
	}
	if text, ok := token.(string); ok && !cleanControl(text) {
		return Invalid("request contains a control character")
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		return inspectObject(decoder, path)
	case '[':
		for index := 0; decoder.More(); index++ {
			indexPath := fmt.Sprintf("%s/%d", path, index)
			if metadataDepth(indexPath) > maxMetadataDepth {
				return Invalid("metadata is nested too deeply")
			}
			if err := inspectValue(decoder, indexPath); err != nil {
				return err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return Invalid("request contains an invalid array")
		}
	}
	return nil
}

func insideMetadata(path string) bool {
	parts := bytes.Split([]byte(path), []byte{'/'})
	for index, part := range parts {
		if string(part) == "metadata" && index < len(parts)-1 {
			return true
		}
	}
	return false
}

func metadataDepth(path string) int {
	parts := strings.Split(path, "/")
	for index, part := range parts {
		if part == "metadata" {
			return len(parts) - 1 - index
		}
	}
	return 0
}

func cleanControl(value string) bool {
	for _, char := range value {
		switch char {
		case '\t', '\n', '\r':
			continue
		}
		if char < 0x20 || char == 0x7f {
			return false
		}
	}
	return true
}
