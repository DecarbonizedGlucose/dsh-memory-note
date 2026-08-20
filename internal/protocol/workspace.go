package protocol

type WorkspaceRegisterRequest struct {
	Path string `json:"path"`
}

type WorkspaceRegisterResponse struct {
	WorkspaceID int64  `json:"workspace_id"`
	Path        string `json:"path"`
	Created     bool   `json:"created"`
}

type WorkspaceRebindRequest struct {
	WorkspaceID int64  `json:"workspace_id"`
	Path        string `json:"path"`
}

type WorkspaceRebindResponse struct {
	WorkspaceID int64  `json:"workspace_id"`
	Path        string `json:"path"`
}

type WorkspaceClearRequest struct {
	WorkspaceID int64 `json:"workspace_id"`
}

type WorkspaceClearResponse struct {
	Deleted int64 `json:"deleted"`
}

type WorkspaceDeleteRequest struct {
	WorkspaceID int64 `json:"workspace_id"`
}

type WorkspaceDeleteResponse struct {
	Deleted bool `json:"deleted"`
}
