package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

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

func Decode(raw string, value any) error {
	if raw == "" {
		return fmt.Errorf("%w: request JSON is required", ErrInvalidRequest)
	}
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("%w: decode request: %v", ErrInvalidRequest, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: request must contain one JSON value", ErrInvalidRequest)
	}
	return nil
}
