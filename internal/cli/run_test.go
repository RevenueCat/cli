package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
	writeJSONError(&buf, &api.APIError{
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

func TestWriteJSONError_HintSurfacesForUnauthorized(t *testing.T) {
	var buf bytes.Buffer
	writeJSONError(&buf, &api.APIError{Status: 401, Type: "unauthorized", Message: "bad key"})
	var got struct {
		Error struct {
			Hint string `json:"hint"`
		} `json:"error"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Error.Hint == "" {
		t.Fatal("expected hint for unauthorized; got empty")
	}
	if !strings.Contains(got.Error.Hint, "rc login") {
		t.Errorf("hint should suggest `rc login`, got %q", got.Error.Hint)
	}
}

func TestWriteJSONError_RetryAfterPropagates(t *testing.T) {
	var buf bytes.Buffer
	writeJSONError(&buf, &api.APIError{
		Status:            429,
		Type:              "rate_limit_exceeded",
		Message:           "slow down",
		RetryAfterSeconds: 30,
	})
	var got struct {
		Error struct {
			RetryAfterSeconds int    `json:"retry_after_seconds"`
			Hint              string `json:"hint"`
		} `json:"error"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Error.RetryAfterSeconds != 30 {
		t.Errorf("want retry_after_seconds=30, got %d", got.Error.RetryAfterSeconds)
	}
	if !strings.Contains(got.Error.Hint, "30 seconds") {
		t.Errorf("hint should mention 30 seconds, got %q", got.Error.Hint)
	}
}

func TestWriteJSONError_CarriesAttachedHint(t *testing.T) {
	var buf bytes.Buffer
	writeJSONError(&buf, WithHint(errors.New("offering already has a paywall"), "delete it first"))
	var got struct {
		Error struct {
			Message string `json:"message"`
			Hint    string `json:"hint"`
		} `json:"error"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, buf.String())
	}
	if got.Error.Message != "offering already has a paywall" {
		t.Errorf("message should be the underlying error, got %q", got.Error.Message)
	}
	if got.Error.Hint != "delete it first" {
		t.Errorf("attached hint should surface in envelope, got %q", got.Error.Hint)
	}
}

func TestHintFor_AttachedHintBeatsAPIFallback(t *testing.T) {
	wrapped := fmt.Errorf("creating draft: %w", &api.APIError{Status: 409, Type: "resource_already_exists", Message: "conflict"})
	if got := hintFor(WithHint(wrapped, "omit --offering-id")); got != "omit --offering-id" {
		t.Errorf("attached hint should win over APIError fallback, got %q", got)
	}
}

func TestWithHint_NilPassesThrough(t *testing.T) {
	if WithHint(nil, "unused") != nil {
		t.Error("WithHint(nil, …) must return nil so it composes in a return position")
	}
}

func TestWriteJSONError_UnknownSubcommandHasOwnType(t *testing.T) {
	var buf bytes.Buffer
	writeJSONError(&buf, &unknownSubcommandError{parent: "rc paywalls", name: "create", suggestion: "generate"})
	var got struct {
		Error struct {
			Type     string `json:"type"`
			Message  string `json:"message"`
			ExitCode int    `json:"exit_code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, buf.String())
	}
	if got.Error.Type != "unknown_command_error" {
		t.Errorf("want type=unknown_command_error, got %q", got.Error.Type)
	}
	if got.Error.ExitCode != 2 {
		t.Errorf("unknown subcommand should map to exit 2, got %d", got.Error.ExitCode)
	}
	if !strings.Contains(got.Error.Message, `did you mean "generate"?`) {
		t.Errorf("message should carry the suggestion, got %q", got.Error.Message)
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
