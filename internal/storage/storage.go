// Package storage centralizes the filesystem behavior that differs across
// platforms — the default data directory, directory syncing, and path case
// normalization — along with the fixed paths under HOME.
package storage

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/protocol"
)

const Env = "DSH_MEMORY_NOTE_HOME"

// Home resolves HOME: the DSH_MEMORY_NOTE_HOME environment variable when set,
// otherwise the platform data directory. The result is absolute.
func Home() (string, error) {
	if value := strings.TrimSpace(os.Getenv(Env)); value != "" {
		if !filepath.IsAbs(value) {
			return "", protocol.Invalid("DSH_MEMORY_NOTE_HOME must be absolute")
		}
		return filepath.Clean(value), nil
	}
	return defaultDataDir()
}

// CanonicalPath resolves a workspace path to the identity stored in the
// database: absolute, cleaned, symlinks resolved, and case-normalized on
// case-insensitive platforms. Registering and resolving both go through it,
// so spellings that differ only by case map to one WID.
func CanonicalPath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", protocol.Invalid("workspace path must be absolute")
	}
	cleaned := filepath.Clean(path)
	info, err := os.Stat(cleaned)
	if err != nil {
		return "", protocol.Invalid("workspace path does not exist")
	}
	if !info.IsDir() {
		return "", protocol.Invalid("workspace path must be a directory")
	}
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		return "", protocol.Invalid("workspace path cannot be resolved")
	}
	return normalizeCase(filepath.Clean(resolved)), nil
}

func MetaDB(homePath string) string    { return filepath.Join(homePath, "meta.db") }
func MemoryDir(homePath string) string { return filepath.Join(homePath, "memory") }

func MemoryDB(homePath string, workspaceID int64) string {
	name := "workspace-" + strconv.FormatInt(workspaceID, 10) + "-memory.db"
	return filepath.Join(MemoryDir(homePath), name)
}

// SyncFile flushes a regular file to disk.
func SyncFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return protocol.NewError(protocol.CodeInternal, "cannot open file for sync")
	}
	defer file.Close()
	if err := file.Sync(); err != nil {
		return protocol.NewError(protocol.CodeInternal, "cannot sync file")
	}
	return nil
}

// SyncDir flushes a directory's entries; it is a no-op on platforms that
// cannot sync directories.
func SyncDir(path string) error { return syncDir(path) }
