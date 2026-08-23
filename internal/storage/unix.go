//go:build !windows

package storage

import (
	"os"
	"path/filepath"

	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/protocol"
)

func defaultDataDir() (string, error) {
	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", protocol.NewError(protocol.CodeInternal, "cannot resolve user data directory")
	}
	return filepath.Join(userHome, ".local", "share", "dsh-memory-note"), nil
}

func syncDir(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return protocol.NewError(protocol.CodeInternal, "cannot open directory for sync")
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return protocol.NewError(protocol.CodeInternal, "cannot sync directory")
	}
	return nil
}

func normalizeCase(path string) string { return path }

func sqlitePath(path string) string { return path }

func executableName(base string) string { return base }
