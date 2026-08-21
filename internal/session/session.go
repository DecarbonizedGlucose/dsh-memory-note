// Package session owns the one-shot invocation Session described by the
// overall design: the highest-level runtime component manager of one
// invocation.
package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"

	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/command"
	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/home"
	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/meta"
	"github.com/DecarbonizedGlucose/dsh-memory-note/internal/protocol"
)

// Session is the highest-level runtime component manager for one one-shot
// invocation. It owns the session ID, the resolved DSH_MEMORY_NOTE_HOME path,
// and a private temporary directory. The SQLite connections, transactions,
// and workspace locks opened by the invocation live inside Run and are
// released under this session ID before Run returns.
type Session struct {
	ID      string
	Home    string
	tempDir string
}

// New resolves and validates DSH_MEMORY_NOTE_HOME and creates a Session at
// it. The Session is not a persisted Harness conversation.
func New() (*Session, error) {
	root, err := home.Root()
	if err != nil {
		return nil, err
	}
	return NewAt(root)
}

// NewAt creates a Session at an explicit home path. It creates the session ID
// and the private temporary directory; it does not touch HOME itself.
func NewAt(home string) (*Session, error) {
	id, err := newID()
	if err != nil {
		return nil, err
	}
	tempDir, err := os.MkdirTemp("", "dsh-memory-note-session-")
	if err != nil {
		return nil, protocol.NewError(protocol.CodeInternal, "cannot create session temporary directory")
	}
	if err := os.Chmod(tempDir, 0o700); err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, protocol.NewError(protocol.CodeInternal, "cannot protect session temporary directory")
	}
	return &Session{ID: id, Home: home, tempDir: tempDir}, nil
}

// TempDir returns the private temporary directory owned by the Session.
func (s *Session) TempDir() string { return s.tempDir }

// Prepare accepts a healthy HOME, initializes a missing HOME atomically, and
// rejects a broken HOME. Run calls it before any business logic.
func (s *Session) Prepare(ctx context.Context) error {
	if _, err := meta.InitRoot(ctx, s.Home); err != nil {
		return err
	}
	return nil
}

// Run executes exactly one subcommand inside this Session. It prepares HOME,
// delegates to the command dispatcher, and returns the single protocol
// response. All locks, databases, and transactions are released before it
// returns; only the temporary directory survives until Close.
func (s *Session) Run(ctx context.Context, name, raw string) protocol.Response {
	if err := s.Prepare(ctx); err != nil {
		return protocol.Failure(err)
	}
	return command.Run(ctx, s.Home, s.ID, name, raw)
}

// Close removes the private temporary directory. It must be attempted on
// every normal return path.
func (s *Session) Close() error { return os.RemoveAll(s.tempDir) }

func newID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", protocol.NewError(protocol.CodeInternal, "cannot create session ID")
	}
	return "session_" + hex.EncodeToString(raw[:]), nil
}
