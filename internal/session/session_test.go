package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/protocol"
)

func TestPrepareInitializesMissingHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	current, err := NewAt(home)
	if err != nil {
		t.Fatal(err)
	}
	defer current.Close()
	if _, err := os.Stat(home); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("NewAt accessed HOME: %v", err)
	}
	if err := current.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	if current.ID == "" || current.Home != home {
		t.Fatalf("session: %#v", current)
	}
	for _, path := range []string{
		filepath.Join(home, "meta.db"),
		filepath.Join(home, "memory"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing initialized path %s: %v", path, err)
		}
	}
	if info, err := os.Stat(current.TempDir()); err != nil || !info.IsDir() {
		t.Fatalf("temporary directory missing: %v", err)
	}
}

func TestPrepareAcceptsHealthyHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	first, err := NewAt(home)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if err := first.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	second, err := NewAt(home)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if err := second.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID {
		t.Fatalf("second session: %#v", second)
	}
}

func TestCloseRemovesTemporaryDirectory(t *testing.T) {
	current, err := NewAt(filepath.Join(t.TempDir(), "home"))
	if err != nil {
		t.Fatal(err)
	}
	tempDir := current.TempDir()
	if err := current.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tempDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary directory was not removed: %v", err)
	}
}

func TestRunInitializesHomeAndExecutes(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := t.TempDir()
	current, err := NewAt(home)
	if err != nil {
		t.Fatal(err)
	}
	defer current.Close()
	response := current.Run(context.Background(), "workspace-resolve", `{"path":"`+project+`"}`)
	if !response.OK || response.Data == nil {
		t.Fatalf("resolve response = %#v", response)
	}
	data, ok := response.Data.(protocol.WorkspaceResolveData)
	if !ok || data.Workspace != nil {
		t.Fatalf("resolve data = %#v", response.Data)
	}
	if _, err := os.Stat(filepath.Join(home, "meta.db")); err != nil {
		t.Fatalf("Run did not initialize HOME: %v", err)
	}
}

func TestRunRejectsBrokenHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	current, err := NewAt(home)
	if err != nil {
		t.Fatal(err)
	}
	defer current.Close()
	if err := current.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	metaDB := filepath.Join(home, "meta.db")
	if err := os.Remove(metaDB); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(metaDB, 0o700); err != nil {
		t.Fatal(err)
	}
	response := current.Run(context.Background(), "workspace-resolve", `{"path":"/tmp/whatever"}`)
	if response.OK || response.Error == nil || response.Error.Code != protocol.CodeHomeBroken {
		t.Fatalf("broken home response = %#v", response)
	}
}
