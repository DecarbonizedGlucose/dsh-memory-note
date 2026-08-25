package protocol

const MaxSafeInteger int64 = 9007199254740991

const (
	MemoryActive     = "active"
	MemorySuperseded = "superseded"
	MemoryInvalid    = "invalid"
)

// MemoryKind enumerates the memory tracks that drive lifecycle and injection.
// fact = a durable conclusion worth injecting into context (a fact, preference,
// decision, constraint, or convention); note = a transient working note that is
// read on demand and never injected.
var MemoryKinds = []string{"fact", "note"}

func ValidMemoryKind(value string) bool {
	for _, kind := range MemoryKinds {
		if kind == value {
			return true
		}
	}
	return false
}

type Memory struct {
	ID           string         `json:"memory_id"`
	WorkspaceID  int64          `json:"workspace_id"`
	Content      string         `json:"content"`
	Kind         string         `json:"kind"`
	Label        *string        `json:"label"`
	Source       []string       `json:"source"`
	Metadata     map[string]any `json:"metadata"`
	State        string         `json:"state"`
	Version      int64          `json:"version"`
	Supersedes   *string        `json:"supersedes"`
	SupersededBy *string        `json:"superseded_by"`
	CreatedAt    Timestamp      `json:"created_at"`
	UpdatedAt    Timestamp      `json:"updated_at"`
}

type MemoryInput struct {
	Content  string         `json:"content"`
	Kind     string         `json:"kind"`
	Label    *string        `json:"label,omitempty"`
	Source   []string       `json:"source,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type SearchFilter struct {
	Kinds         []string   `json:"kinds,omitempty"`
	Labels        []string   `json:"labels,omitempty"`
	CreatedAfter  *Timestamp `json:"created_after,omitempty"`
	CreatedBefore *Timestamp `json:"created_before,omitempty"`
	UpdatedAfter  *Timestamp `json:"updated_after,omitempty"`
	UpdatedBefore *Timestamp `json:"updated_before,omitempty"`
}

type SearchHit struct {
	ID           string    `json:"memory_id"`
	Kind         string    `json:"kind"`
	Label        *string   `json:"label"`
	Version      int64     `json:"version"`
	Snippet      string    `json:"snippet"`
	Score        float64   `json:"score"`
	MatchedTerms int       `json:"matched_terms"`
	UpdatedAt    Timestamp `json:"updated_at"`
}

// ListItem is the compact row returned by memory-list.
type ListItem struct {
	ID           string    `json:"memory_id"`
	Kind         string    `json:"kind"`
	Label        *string   `json:"label"`
	State        string    `json:"state"`
	Version      int64     `json:"version"`
	Supersedes   *string   `json:"supersedes"`
	SupersededBy *string   `json:"superseded_by"`
	CreatedAt    Timestamp `json:"created_at"`
	UpdatedAt    Timestamp `json:"updated_at"`
}

type MemoryTarget struct {
	WorkspaceID     int64  `json:"workspace_id"`
	MemoryID        string `json:"memory_id"`
	ExpectedVersion int64  `json:"expected_version"`
}

type MemoryResult struct {
	Memory Memory `json:"memory"`
}

type MemorySearchRequest struct {
	WorkspaceID int64         `json:"workspace_id"`
	Query       string        `json:"query,omitempty"`
	Filter      *SearchFilter `json:"filter,omitempty"`
	Limit       *int          `json:"limit,omitempty"`
}
type MemorySearchData struct {
	Memories []SearchHit `json:"memories"`
}

type MemoryListRequest struct {
	WorkspaceID int64   `json:"workspace_id"`
	Limit       *int    `json:"limit,omitempty"`
	Cursor      *string `json:"cursor,omitempty"`
}
type MemoryListData struct {
	Memories   []ListItem `json:"memories"`
	NextCursor *string    `json:"next_cursor,omitempty"`
}

type MemoryGetRequest struct {
	WorkspaceID int64  `json:"workspace_id"`
	MemoryID    string `json:"memory_id"`
	Version     *int64 `json:"version,omitempty"`
}
type MemoryGetData struct{ MemoryResult }

// HistoryItem is one version row of memory-history.
type HistoryItem struct {
	Version    int64      `json:"version"`
	Action     string     `json:"action"`
	State      string     `json:"state"`
	UpdatedAt  Timestamp  `json:"updated_at"`
	ArchivedAt *Timestamp `json:"archived_at"`
}

type MemoryHistoryRequest struct {
	WorkspaceID int64  `json:"workspace_id"`
	MemoryID    string `json:"memory_id"`
}
type MemoryHistoryData struct {
	Versions []HistoryItem `json:"versions"`
}

type MemoryDiffRequest struct {
	WorkspaceID int64  `json:"workspace_id"`
	MemoryID    string `json:"memory_id"`
	FromVersion int64  `json:"from_version"`
	ToVersion   int64  `json:"to_version"`
}

type MemoryDiffChange struct {
	Field string `json:"field"`
	From  any    `json:"from"`
	To    any    `json:"to"`
}

type MemoryDiffData struct {
	FromVersion int64              `json:"from_version"`
	ToVersion   int64              `json:"to_version"`
	Changes     []MemoryDiffChange `json:"changes"`
}

type MemoryCreateRequest struct {
	WorkspaceID int64 `json:"workspace_id"`
	MemoryInput
	Reason string `json:"reason,omitempty"`
}
type MemoryCreateData struct{ MemoryResult }

type MemoryUpdateRequest struct {
	WorkspaceID     int64           `json:"workspace_id"`
	MemoryID        string          `json:"memory_id"`
	ExpectedVersion int64           `json:"expected_version"`
	Content         *string         `json:"content,omitempty"`
	Kind            *string         `json:"kind,omitempty"`
	Label           *string         `json:"label,omitempty"`
	Source          *[]string       `json:"source,omitempty"`
	Metadata        *map[string]any `json:"metadata,omitempty"`
	Reason          string          `json:"reason,omitempty"`
}
type MemoryUpdateData struct{ MemoryResult }

type MemorySupersedeRequest struct {
	MemoryTarget
	New    MemoryInput `json:"new"`
	Reason string      `json:"reason,omitempty"`
}
type MemorySupersedeData struct {
	Old Memory `json:"old"`
	New Memory `json:"new"`
}

type MemoryInvalidateRequest struct {
	MemoryTarget
	Reason string `json:"reason,omitempty"`
}
type MemoryInvalidateData struct{ MemoryResult }

type MemoryDeleteRequest struct{ MemoryTarget }
type MemoryDeleteData struct {
	Deleted bool `json:"deleted"`
}
