package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/decglu/dsh-memory-note/internal/data"
	"github.com/decglu/dsh-memory-note/internal/meta"
	"github.com/decglu/dsh-memory-note/internal/rpc"
)

type Session struct {
	ID     string
	Home   string
	Server rpc.Server
}

func New() (*Session, error) {
	home, err := data.Home()
	if err != nil {
		return nil, err
	}
	return NewAt(home)
}

// NewAt creates a session at an explicit home.
func NewAt(home string) (*Session, error) {
	id, err := newID()
	if err != nil {
		return nil, err
	}
	return &Session{
		ID:     id,
		Home:   home,
		Server: rpc.NewServer(home, id),
	}, nil
}

// Check accepts a healthy HOME, initializes a missing HOME, and rejects a
// broken HOME. Server must only be used after this method succeeds.
func (s *Session) Check(ctx context.Context) error {
	_, err := meta.PrepareHome(ctx, s.Home, s.ID)
	return err
}

func newID() (string, error) {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("create session id: %w", err)
	}
	return "session_" + hex.EncodeToString(raw[:]), nil
}
