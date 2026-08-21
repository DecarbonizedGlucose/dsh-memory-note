// Package home resolves HOME (DSH_MEMORY_NOTE_HOME) and the fixed database
// paths under it. Per the overall design, HOME means DSH_MEMORY_NOTE_HOME,
// not the operating-system $HOME directory.
package home

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/protocol"
)

const Env = "DSH_MEMORY_NOTE_HOME"

// Root resolves the configured HOME: the DSH_MEMORY_NOTE_HOME environment
// variable when set, otherwise ~/.local/dsh-memory-note. It must be absolute.
func Root() (string, error) {
	if value := strings.TrimSpace(os.Getenv(Env)); value != "" {
		if !filepath.IsAbs(value) {
			return "", protocol.Invalid("DSH_MEMORY_NOTE_HOME must be absolute")
		}
		return filepath.Clean(value), nil
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", protocol.NewError(protocol.CodeInternal, "cannot resolve user data directory")
	}
	return filepath.Join(userHome, ".local", "dsh-memory-note"), nil
}

func MetaDB(homePath string) string    { return filepath.Join(homePath, "meta.db") }
func MemoryDir(homePath string) string { return filepath.Join(homePath, "memory") }

func MemoryDB(homePath string, workspaceID int64) string {
	name := "workspace-" + strconv.FormatInt(workspaceID, 10) + "-memory.db"
	return filepath.Join(MemoryDir(homePath), name)
}
