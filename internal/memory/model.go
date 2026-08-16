package memory

import (
	"errors"
	"time"
)

var (
	ErrNotFound       = errors.New("memory not found")
	ErrConflict       = errors.New("memory version conflict")
	ErrInvalidState   = errors.New("memory state does not allow this operation")
	ErrInvalidRequest = errors.New("invalid memory request")
)

type State string

const (
	Active     State = "active"
	Superseded State = "superseded"
	Invalid    State = "invalid"
)

type Memory struct {
	ID           string         `json:"memory_id"`
	WorkspaceID  int64          `json:"workspace_id"`
	Content      string         `json:"content"`
	Type         string         `json:"type,omitempty"`
	Scope        string         `json:"scope,omitempty"`
	Source       []string       `json:"source,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	State        State          `json:"state"`
	Version      int            `json:"version"`
	Supersedes   string         `json:"supersedes,omitempty"`
	SupersededBy string         `json:"superseded_by,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

type CreateInput struct {
	Content  string         `json:"content"`
	Type     string         `json:"type,omitempty"`
	Scope    string         `json:"scope,omitempty"`
	Source   []string       `json:"source,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type UpdateInput struct {
	ID              string          `json:"memory_id"`
	ExpectedVersion int             `json:"expected_version"`
	Content         *string         `json:"content,omitempty"`
	Type            *string         `json:"type,omitempty"`
	Scope           *string         `json:"scope,omitempty"`
	Source          *[]string       `json:"source,omitempty"`
	Metadata        *map[string]any `json:"metadata,omitempty"`
}

type Filter struct {
	Types         []string   `json:"types,omitempty"`
	Scopes        []string   `json:"scopes,omitempty"`
	CreatedAfter  *time.Time `json:"created_after,omitempty"`
	CreatedBefore *time.Time `json:"created_before,omitempty"`
	UpdatedAfter  *time.Time `json:"updated_after,omitempty"`
	UpdatedBefore *time.Time `json:"updated_before,omitempty"`
}

type SearchInput struct {
	Query  string `json:"query,omitempty"`
	Filter Filter `json:"filter,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type SearchHit struct {
	ID        string    `json:"memory_id"`
	Type      string    `json:"type,omitempty"`
	Scope     string    `json:"scope,omitempty"`
	State     State     `json:"state"`
	Version   int       `json:"version"`
	Snippet   string    `json:"snippet"`
	Score     float64   `json:"score"`
	UpdatedAt time.Time `json:"updated_at"`
}

type SupersedeResult struct {
	Old Memory `json:"old"`
	New Memory `json:"new"`
}
