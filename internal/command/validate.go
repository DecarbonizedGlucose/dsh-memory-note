package command

import (
	"encoding/json"
	"fmt"
	"reflect"
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
	if !protocol.ValidMemoryKind(strings.TrimSpace(input.Kind)) {
		return protocol.MemoryInput{}, protocol.Invalid("kind must be fact or note")
	}
	input.Kind = strings.TrimSpace(input.Kind)
	var err error
	input.Label, err = cleanLabel(input.Label, 256, "label")
	if err != nil {
		return protocol.MemoryInput{}, err
	}
	input.Branches, err = cleanBranches(input.Branches)
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

func cleanReason(value string) (string, error) {
	cleaned := strings.TrimSpace(value)
	if len(cleaned) > 512 {
		return "", protocol.Invalid("reason is too long")
	}
	return cleaned, nil
}

// cleanBranches trims, de-duplicates, and drops empty branch names. An empty
// result means "visible on all branches" and is stored as nil.
func cleanBranches(values []string) ([]string, error) {
	seen := make(map[string]bool)
	result := make([]string, 0, len(values))
	for index, value := range values {
		value = strings.TrimSpace(value)
		if len(value) > 256 {
			return nil, protocol.Invalid(fmt.Sprintf("branch item %d is too long", index))
		}
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	if len(result) > 64 {
		return nil, protocol.Invalid("branches has too many items")
	}
	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

// matchesBranch reports whether a memory with the given branch restriction is
// visible under the requested branch. A nil request branch or a nil/empty
// restriction means "all branches".
func matchesBranch(branches []string, branch *string) bool {
	if branch == nil || len(branches) == 0 {
		return true
	}
	for _, candidate := range branches {
		if candidate == *branch {
			return true
		}
	}
	return false
}

// diffMemories compares two versions of the same memory and returns only the
// fields that differ, each with from and to.
func diffMemories(from, to protocol.Memory) []protocol.MemoryDiffChange {
	changes := make([]protocol.MemoryDiffChange, 0, 7)
	compare := func(field string, fromValue, toValue any) {
		if !reflect.DeepEqual(fromValue, toValue) {
			changes = append(changes, protocol.MemoryDiffChange{Field: field, From: fromValue, To: toValue})
		}
	}
	compare("content", from.Content, to.Content)
	compare("kind", from.Kind, to.Kind)
	compare("label", from.Label, to.Label)
	compare("branches", from.Branches, to.Branches)
	compare("source", from.Source, to.Source)
	compare("metadata", from.Metadata, to.Metadata)
	compare("state", from.State, to.State)
	return changes
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

func words(value string) []string {
	parts := strings.FieldsFunc(asciiFold(value), func(char rune) bool {
		return !unicode.IsLetter(char) && !unicode.IsNumber(char)
	})
	return cleanLabels(parts)
}

func asciiFold(value string) string {
	folded := []byte(value)
	for index, char := range folded {
		if char >= 'A' && char <= 'Z' {
			folded[index] = char + 32
		}
	}
	return string(folded)
}
