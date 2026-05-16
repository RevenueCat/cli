package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

// writeJSONError is the linchpin of the agent error contract: when the user
// passes --json and a command fails, the error envelope on stderr must match
// the shape of API errors so one parser handles both.

func TestWriteJSONError_PlainError(t *testing.T) {
	var buf bytes.Buffer
	writeJSONError(&buf, errors.New("something went wrong"))
	var got struct {
		Error struct {
			Type     string `json:"type"`
			Message  string `json:"message"`
			ExitCode int    `json:"exit_code"`
		} `json:"error"`
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, buf.String())
	}
	if got.Error.Type != "cli_error" {
		t.Errorf("want type=cli_error for generic, got %q", got.Error.Type)
	}
	if got.Error.ExitCode != 1 {
		t.Errorf("want exit_code=1 for generic, got %d", got.Error.ExitCode)
	}
	if got.SchemaVersion != 1 {
		t.Errorf("want schema_version=1, got %d", got.SchemaVersion)
	}
}

func TestWriteJSONError_APIErrorPreservesContext(t *testing.T) {
	var buf bytes.Buffer
	writeJSONError(&buf, &api.Error{
		Status:    404,
		Type:      "resource_missing",
		Message:   "entitlement entl_x not found",
		RequestID: "req_abc",
		DocURL:    "https://errors.rev.cat/resource-missing",
	})
	var got struct {
		Error struct {
			Type      string `json:"type"`
			Message   string `json:"message"`
			ExitCode  int    `json:"exit_code"`
			RequestID string `json:"request_id"`
			DocURL    string `json:"doc_url"`
		} `json:"error"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if got.Error.Type != "resource_missing" {
		t.Errorf("API error type should propagate, got %q", got.Error.Type)
	}
	if got.Error.ExitCode != 5 {
		t.Errorf("resource_missing should map to exit 5, got %d", got.Error.ExitCode)
	}
	if got.Error.RequestID != "req_abc" {
		t.Errorf("request_id must propagate for agent debugging, got %q", got.Error.RequestID)
	}
	if got.Error.DocURL == "" {
		t.Errorf("doc_url should propagate, got empty")
	}
}

func TestWriteJSONError_NotAuthenticatedHasUnauthorizedType(t *testing.T) {
	var buf bytes.Buffer
	writeJSONError(&buf, ErrNotAuthenticated)
	var got struct {
		Error struct {
			Type     string `json:"type"`
			ExitCode int    `json:"exit_code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Error.Type != "unauthorized" {
		t.Errorf("not-authenticated should map to unauthorized, got %q", got.Error.Type)
	}
	if got.Error.ExitCode != 4 {
		t.Errorf("not-authenticated should be exit 4, got %d", got.Error.ExitCode)
	}
}
