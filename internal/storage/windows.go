//go:build windows

package storage

import (
	"os"
	"path/filepath"

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

// normalizeCase leaves the path unchanged: filepath.EvalSymlinks already
// canonicalizes each component to its on-disk case on Windows, so lowercasing
// here would only destroy the user's case while adding nothing.
func normalizeCase(path string) string { return path }

// sqlitePath renders an absolute Windows path as the forward-slash, leading
// slash form SQLite's file: URI expects (e.g. "/C:/Users/.../meta.db").
func sqlitePath(path string) string {
	path = filepath.ToSlash(path)
	if len(path) >= 2 && path[1] == ':' {
		path = "/" + path
	}
	return path
}

func executableName(base string) string { return base + ".exe" }
