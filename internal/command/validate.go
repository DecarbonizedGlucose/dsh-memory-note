package command

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/protocol"
)

func validWorkspaceID(value int64) error {
	if value < 1 || value > protocol.MaxSafeInteger {
		return protocol.Invalid("workspace_id must be a positive safe integer")
	}
	return nil
}

func validVersion(value int64) error {
	if value < 1 || value > protocol.MaxSafeInteger {
		return protocol.Invalid("expected_version must be a positive safe integer")
	}
	return nil
}

func validMemoryID(value string) error {
	if strings.TrimSpace(value) == "" || len(value) > 128 || strings.ContainsRune(value, 0) {
		return protocol.Invalid("memory_id is invalid")
	}
	return nil
}

func cleanInput(input protocol.MemoryInput) (protocol.MemoryInput, error) {
	if !utf8.ValidString(input.Content) || strings.TrimSpace(input.Content) == "" ||
		strings.ContainsRune(input.Content, 0) || len(input.Content) > 64*1024 {
		return protocol.MemoryInput{}, protocol.Invalid("content is invalid")
	}
	var err error
	input.Type, err = cleanLabel(input.Type, 128, "type")
	if err != nil {
		return protocol.MemoryInput{}, err
	}
	input.Scope, err = cleanLabel(input.Scope, 256, "scope")
	if err != nil {
		return protocol.MemoryInput{}, err
	}
	input.Source, err = cleanSource(input.Source)
	if err != nil {
		return protocol.MemoryInput{}, err
	}
	if input.Metadata == nil {
		input.Metadata = map[string]any{}
	}
	encoded, err := json.Marshal(input.Metadata)
	if err != nil || len(encoded) > 16*1024 {
		return protocol.MemoryInput{}, protocol.Invalid("metadata is invalid or too large")
	}
	return input, nil
}

func cleanLabel(value *string, limit int, name string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	cleaned := strings.TrimSpace(*value)
	if cleaned == "" {
		return nil, nil
	}
	if len(cleaned) > limit {
		return nil, protocol.Invalid(name + " is too long")
	}
	return &cleaned, nil
}

func cleanSource(values []string) ([]string, error) {
	seen := make(map[string]bool)
	result := make([]string, 0, len(values))
	for index, value := range values {
		value = strings.TrimSpace(value)
		if len(value) > 512 {
			return nil, protocol.Invalid(fmt.Sprintf("source item %d is too long", index))
		}
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	if len(result) > 64 {
		return nil, protocol.Invalid("source has too many items")
	}
	return result, nil
}

func cleanLabels(values []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

// words splits the query into keywords: maximal runs of Unicode letters or
// numbers, with ASCII-only case folding. No Unicode normalization is applied.
func words(value string) []string {
	parts := strings.FieldsFunc(asciiFold(value), func(char rune) bool {
		return !unicode.IsLetter(char) && !unicode.IsNumber(char)
	})
	return cleanLabels(parts)
}

// asciiFold lowercases only ASCII A-Z; every other byte is kept exactly.
func asciiFold(value string) string {
	folded := []byte(value)
	for index, char := range folded {
		if char >= 'A' && char <= 'Z' {
			folded[index] = char + 32
		}
	}
	return string(folded)
}
