// package data provides functions to locate the data directory and database files.
//
// The data directory is normally ~/.local/dsh-memory-note, but can be overridden.
//
// DSH_MEMORY_NOTE_HOME/
//     meta.db
//     memory/
//         workspace-{workspace_id}-memory.db
//         ...

package data

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const HomeEnv = "DSH_MEMORY_NOTE_HOME"

// Home returns the data directory. Normal
// installs use ~/.local/dsh-memory-note.
//
// Allow override with the DSH_MEMORY_NOTE_HOME environment variable.
func Home() (string, error) {
	if value := strings.TrimSpace(os.Getenv(HomeEnv)); value != "" {
		return filepath.Abs(value)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find user home: %w", err)
	}
	return filepath.Join(home, ".local", "dsh-memory-note"), nil
}

func MetaDB(home string) string {
	return filepath.Join(home, "meta.db")
}

func MemoryDir(home string) string {
	return filepath.Join(home, "memory")
}

func MemoryDB(home string, workspaceID int64) (string, error) {
	if workspaceID < 0 {
		return "", fmt.Errorf("invalid workspace_id")
	}
	id := strconv.FormatInt(workspaceID, 10)
	return filepath.Join(MemoryDir(home), "workspace-"+id+"-memory.db"), nil
}
