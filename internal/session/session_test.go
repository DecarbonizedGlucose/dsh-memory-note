package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/protocol"
	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/rpc"
)

func TestCheckInitializesMissingHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	current, err := NewAt(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(home); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("New accessed HOME: %v", err)
	}
	if err := current.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if current.Server != rpc.NewServer(home, current.ID) {
		t.Fatalf("session does not own the expected server: %#v", current)
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
}

func TestCheckAcceptsHealthyHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	first, err := NewAt(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	second, err := NewAt(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID {
		t.Fatalf("second session: %#v", second)
	}
}

func TestCheckRejectsBrokenHome(t *testing.T) {
	tests := []struct {
		name  string
		setup func(string) error
	}{
		{
			name: "missing meta db",
			setup: func(home string) error {
				return os.Mkdir(filepath.Join(home, "memory"), 0o700)
			},
		},
		{
			name: "missing memory directory",
			setup: func(home string) error {
				return os.WriteFile(filepath.Join(home, "meta.db"), nil, 0o600)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			home := filepath.Join(t.TempDir(), "home")
			if err := os.Mkdir(home, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := test.setup(home); err != nil {
				t.Fatal(err)
			}
			current, err := NewAt(home)
			if err != nil {
				t.Fatal(err)
			}
			if err := current.Check(context.Background()); !errors.Is(err, protocol.ErrHomeBroken) {
				t.Fatalf("expected broken home error, got %v", err)
			}
		})
	}
}
