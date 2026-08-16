package rpc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/decglu/dsh-memory-note/internal/memory"
	"github.com/decglu/dsh-memory-note/internal/meta"
)

var errBadRequest = errors.New("invalid request")

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Reply struct {
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error *Error `json:"error,omitempty"`
}

func Success(data any) Reply {
	return Reply{OK: true, Data: data}
}

func Failure(err error) Reply {
	return Reply{Error: &Error{Code: errorCode(err), Message: err.Error()}}
}

func decode(raw string, value any) error {
	if raw == "" {
		return fmt.Errorf("%w: request JSON is required", errBadRequest)
	}
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("%w: decode request: %v", errBadRequest, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: request must contain one JSON value", errBadRequest)
	}
	return nil
}

func errorCode(err error) string {
	switch {
	case errors.Is(err, meta.ErrHomeBroken):
		return "home_broken"
	case errors.Is(err, errBadRequest):
		return "invalid_request"
	case errors.Is(err, meta.ErrNotFound):
		return "workspace_not_found"
	case errors.Is(err, meta.ErrPathUsed):
		return "workspace_path_used"
	case errors.Is(err, meta.ErrBusy):
		return "workspace_busy"
	case errors.Is(err, memory.ErrNotFound):
		return "memory_not_found"
	case errors.Is(err, memory.ErrConflict):
		return "version_conflict"
	case errors.Is(err, memory.ErrInvalidState):
		return "invalid_memory_state"
	case errors.Is(err, memory.ErrInvalidRequest):
		return "invalid_request"
	default:
		return "error"
	}
}
