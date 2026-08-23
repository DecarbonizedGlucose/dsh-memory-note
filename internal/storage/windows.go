//go:build windows

package storage

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/protocol"
)

func defaultDataDir() (string, error) {
	if dir := os.Getenv("LOCALAPPDATA"); dir != "" {
		return filepath.Join(dir, "dsh-memory-note"), nil
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", protocol.NewError(protocol.CodeInternal, "cannot resolve user data directory")
	}
	return filepath.Join(userHome, "AppData", "Local", "dsh-memory-note"), nil
}

func syncDir(string) error { return nil }

func normalizeCase(path string) string { return strings.ToLower(path) }
