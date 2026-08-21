package protocol

type Workspace struct {
	ID        int64     `json:"workspace_id"`
	Path      string    `json:"path"`
	CreatedAt Timestamp `json:"created_at"`
	UpdatedAt Timestamp `json:"updated_at"`
}

type WorkspacePath struct {
	Path string `json:"path"`
}

type WorkspaceTarget struct {
	WorkspaceID int64 `json:"workspace_id"`
}

type WorkspaceResolveRequest struct{ WorkspacePath }
type WorkspaceResolveData struct {
	Workspace *Workspace `json:"workspace"`
}

type WorkspaceRegisterRequest struct{ WorkspacePath }
type WorkspaceRegisterData struct {
	Workspace Workspace `json:"workspace"`
	Created   bool      `json:"created"`
}

type WorkspaceRebindRequest struct {
	WorkspaceID int64  `json:"workspace_id"`
	Path        string `json:"path"`
}
type WorkspaceRebindData struct {
	Workspace Workspace `json:"workspace"`
}

type WorkspaceClearRequest struct{ WorkspaceTarget }
type WorkspaceClearData struct {
	DeletedCount int64 `json:"deleted_count"`
}

type WorkspaceDeleteRequest struct{ WorkspaceTarget }
type WorkspaceDeleteData struct {
	Deleted bool `json:"deleted"`
}
