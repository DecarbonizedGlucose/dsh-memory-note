package protocol

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDecodeStrictJSON(t *testing.T) {
	valid := `{"workspace_id":1,"content":"SQLite","metadata":{"nullable":null,"nested":{"x":1}}}`
	var request MemoryCreateRequest
	if err := Decode(valid, &request); err != nil {
		t.Fatal(err)
	}
	if request.WorkspaceID != 1 || request.Content != "SQLite" || request.Metadata["nullable"] != nil {
		t.Fatalf("decoded request = %#v", request)
	}

	invalid := []string{
		`[]`,
		`{"workspace_id":1,"workspace_id":2,"content":"x"}`,
		`{"workspace_id":1,"content":"x","unknown":true}`,
		`{"workspace_id":1,"content":null}`,
		`{"workspace_id":1.0,"content":"x"}`,
		`{"workspace_id":1e0,"content":"x"}`,
		`{"workspace_id":1,"content":"x"} {}`,
	}
	for _, raw := range invalid {
		request = MemoryCreateRequest{}
		err := Decode(raw, &request)
		if err == nil || err.(*Error).Code != CodeInvalidRequest {
			t.Fatalf("Decode(%s) = %v", raw, err)
		}
	}
}

func TestDecodeRejectsInvalidUTF8AndOversize(t *testing.T) {
	badUTF8 := string([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'})
	var request WorkspaceResolveRequest
	if err := Decode(badUTF8, &request); err == nil {
		t.Fatal("expected invalid UTF-8 error")
	}
	if err := Decode(string(make([]byte, MaxRequestSize+1)), &request); err == nil {
		t.Fatal("expected request size error")
	}
}

func TestDecodeRejectsControlCharacters(t *testing.T) {
	valid := []string{
		`{"workspace_id":1,"content":"tab\there\nnext\rline"}`,
	}
	for _, raw := range valid {
		var request MemoryCreateRequest
		if err := Decode(raw, &request); err != nil {
			t.Fatalf("Decode(%s) = %v", raw, err)
		}
	}
	invalid := []string{
		`{"workspace_id":1,"content":"bad\u0001char"}`,
		`{"workspace_id":1,"content":"bad\u0000char"}`,
		`{"workspace_id":1,"content":"bad\u007fchar"}`,
		`{"workspace_id":1,"content":"x","metadata":{"bad\u0002key":1}}`,
	}
	for _, raw := range invalid {
		var request MemoryCreateRequest
		err := Decode(raw, &request)
		if err == nil || err.(*Error).Code != CodeInvalidRequest {
			t.Fatalf("Decode(%s) = %v", raw, err)
		}
	}
}

func TestDecodeRejectsDeepMetadata(t *testing.T) {
	deep := `{"workspace_id":1,"content":"x","metadata":`
	deep += strings.Repeat(`{"x":`, 33)
	deep += `1`
	deep += strings.Repeat(`}`, 33)
	deep += `}`
	var request MemoryCreateRequest
	err := Decode(deep, &request)
	if err == nil || err.(*Error).Code != CodeInvalidRequest {
		t.Fatalf("Decode(deep metadata) = %v", err)
	}

	ok := `{"workspace_id":1,"content":"x","metadata":`
	ok += strings.Repeat(`{"x":`, 32)
	ok += `1`
	ok += strings.Repeat(`}`, 32)
	ok += `}`
	if err := Decode(ok, &request); err != nil {
		t.Fatalf("Decode(32-level metadata) = %v", err)
	}
}

func TestTimestampRoundTrip(t *testing.T) {
	input := `"2026-08-21T01:02:03+08:00"`
	var timestamp Timestamp
	if err := json.Unmarshal([]byte(input), &timestamp); err != nil {
		t.Fatal(err)
	}
	output, err := json.Marshal(timestamp)
	if err != nil || string(output) != input {
		t.Fatalf("round trip = %s, %v", output, err)
	}
	zulu, err := json.Marshal(Timestamp{Time: time.Unix(1787274123, 0).In(time.FixedZone("", 0))})
	if err != nil || string(zulu) != `"2026-08-21T01:02:03+00:00"` {
		t.Fatalf("zero offset = %s, %v", zulu, err)
	}
	for _, bad := range []string{
		`"2026-08-21T01:02:03"`,
		`"2026-08-21T01:02:03.5Z"`,
		`"2026-08-21T01:02:03+0800"`,
		`"2026-08-21T01:02:03z"`,
	} {
		if err := json.Unmarshal([]byte(bad), &timestamp); err == nil {
			t.Fatalf("timestamp %s accepted", bad)
		}
	}
}

func TestResponseShapes(t *testing.T) {
	success, _ := json.Marshal(Success(WorkspaceDeleteData{Deleted: true}))
	if string(success) != `{"ok":true,"data":{"deleted":true}}` {
		t.Fatalf("success = %s", success)
	}
	failure, _ := json.Marshal(Failure(NewError(CodeVersionConflict, "version changed")))
	if string(failure) != `{"ok":false,"error":{"code":"version_conflict","message":"version changed"}}` {
		t.Fatalf("failure = %s", failure)
	}
}
