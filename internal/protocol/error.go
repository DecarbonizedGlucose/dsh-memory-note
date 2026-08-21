package protocol

const (
	CodeInvalidRequest     = "invalid_request"
	CodeHomeBroken         = "home_broken"
	CodeSchemaMismatch     = "schema_mismatch"
	CodeWorkspaceNotFound  = "workspace_not_found"
	CodeWorkspacePathUsed  = "workspace_path_used"
	CodeWorkspaceBusy      = "workspace_busy"
	CodeWorkspaceBroken    = "workspace_broken"
	CodeMemoryNotFound     = "memory_not_found"
	CodeVersionConflict    = "version_conflict"
	CodeInvalidMemoryState = "invalid_memory_state"
	CodeInternal           = "internal_error"
)

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Message }

type Response struct {
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error *Error `json:"error,omitempty"`
}

func Success(data any) Response { return Response{OK: true, Data: data} }

func Failure(err error) Response {
	if appError, ok := err.(*Error); ok {
		return Response{OK: false, Error: appError}
	}
	return Response{OK: false, Error: NewError(CodeInternal, "internal error")}
}

func NewError(code, message string) *Error { return &Error{Code: code, Message: message} }

func Invalid(message string) *Error { return NewError(CodeInvalidRequest, message) }
