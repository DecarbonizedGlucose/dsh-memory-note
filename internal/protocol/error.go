package protocol

import "errors"

var (
	ErrInvalidRequest     = errors.New("invalid request")
	ErrHomeBroken         = errors.New("DSH_MEMORY_NOTE_HOME is broken; meta.db or memory directory is missing; all data is untrusted; clean the directory manually")
	ErrWorkspaceNotFound  = errors.New("workspace not found")
	ErrWorkspacePathUsed  = errors.New("workspace path is already registered")
	ErrWorkspaceBusy      = errors.New("workspace is busy")
	ErrMemoryNotFound     = errors.New("memory not found")
	ErrVersionConflict    = errors.New("memory version conflict")
	ErrInvalidMemoryState = errors.New("memory state does not allow this operation")
)

func errorCode(err error) string {
	switch {
	case errors.Is(err, ErrHomeBroken):
		return "home_broken"
	case errors.Is(err, ErrInvalidRequest):
		return "invalid_request"
	case errors.Is(err, ErrWorkspaceNotFound):
		return "workspace_not_found"
	case errors.Is(err, ErrWorkspacePathUsed):
		return "workspace_path_used"
	case errors.Is(err, ErrWorkspaceBusy):
		return "workspace_busy"
	case errors.Is(err, ErrMemoryNotFound):
		return "memory_not_found"
	case errors.Is(err, ErrVersionConflict):
		return "version_conflict"
	case errors.Is(err, ErrInvalidMemoryState):
		return "invalid_memory_state"
	default:
		return "error"
	}
}
