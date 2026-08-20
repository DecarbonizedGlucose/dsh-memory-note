package protocol

import "time"

type MemoryState string

const (
	MemoryActive     MemoryState = "active"
	MemorySuperseded MemoryState = "superseded"
	MemoryInvalid    MemoryState = "invalid"
)

type Memory struct {
	ID           string         `json:"memory_id"`
	WorkspaceID  int64          `json:"workspace_id"`
	Content      string         `json:"content"`
	Type         string         `json:"type,omitempty"`
	Scope        string         `json:"scope,omitempty"`
	Source       []string       `json:"source,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	State        MemoryState    `json:"state"`
	Version      int            `json:"version"`
	Supersedes   string         `json:"supersedes,omitempty"`
	SupersededBy string         `json:"superseded_by,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

type MemoryCreate struct {
	Content  string         `json:"content"`
	Type     string         `json:"type,omitempty"`
	Scope    string         `json:"scope,omitempty"`
	Source   []string       `json:"source,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type MemoryFilter struct {
	Types         []string   `json:"types,omitempty"`
	Scopes        []string   `json:"scopes,omitempty"`
	CreatedAfter  *time.Time `json:"created_after,omitempty"`
	CreatedBefore *time.Time `json:"created_before,omitempty"`
	UpdatedAfter  *time.Time `json:"updated_after,omitempty"`
	UpdatedBefore *time.Time `json:"updated_before,omitempty"`
}

type MemorySearchRequest struct {
	WorkspaceID int64        `json:"workspace_id"`
	Query       string       `json:"query,omitempty"`
	Filter      MemoryFilter `json:"filter,omitempty"`
	Limit       int          `json:"limit,omitempty"`
}

type MemorySearchHit struct {
	ID        string      `json:"memory_id"`
	Type      string      `json:"type,omitempty"`
	Scope     string      `json:"scope,omitempty"`
	State     MemoryState `json:"state"`
	Version   int         `json:"version"`
	Snippet   string      `json:"snippet"`
	Score     float64     `json:"score"`
	UpdatedAt time.Time   `json:"updated_at"`
}

type MemorySearchResponse struct {
	Memories []MemorySearchHit `json:"memories"`
}

type MemoryGetRequest struct {
	WorkspaceID int64  `json:"workspace_id"`
	MemoryID    string `json:"memory_id"`
}

type MemoryGetResponse struct {
	Memory Memory `json:"memory"`
}

type MemoryCreateRequest struct {
	WorkspaceID int64 `json:"workspace_id"`
	MemoryCreate
}

type MemoryCreateResponse struct {
	Memory Memory `json:"memory"`
}

type MemoryUpdateRequest struct {
	WorkspaceID     int64           `json:"workspace_id"`
	MemoryID        string          `json:"memory_id"`
	ExpectedVersion int             `json:"expected_version"`
	Content         *string         `json:"content,omitempty"`
	Type            *string         `json:"type,omitempty"`
	Scope           *string         `json:"scope,omitempty"`
	Source          *[]string       `json:"source,omitempty"`
	Metadata        *map[string]any `json:"metadata,omitempty"`
}

type MemoryUpdateResponse struct {
	Memory Memory `json:"memory"`
}

type MemorySupersedeRequest struct {
	WorkspaceID     int64        `json:"workspace_id"`
	MemoryID        string       `json:"memory_id"`
	ExpectedVersion int          `json:"expected_version"`
	New             MemoryCreate `json:"new"`
}

type MemorySupersedeResponse struct {
	Old Memory `json:"old"`
	New Memory `json:"new"`
}

type MemoryInvalidateRequest struct {
	WorkspaceID     int64  `json:"workspace_id"`
	MemoryID        string `json:"memory_id"`
	ExpectedVersion int    `json:"expected_version"`
}

type MemoryInvalidateResponse struct {
	Memory Memory `json:"memory"`
}

type MemoryDeleteRequest struct {
	WorkspaceID     int64  `json:"workspace_id"`
	MemoryID        string `json:"memory_id"`
	ExpectedVersion int    `json:"expected_version"`
}

type MemoryDeleteResponse struct {
	Deleted bool `json:"deleted"`
}
