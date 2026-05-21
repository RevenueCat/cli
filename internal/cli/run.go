package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/revenuecat/cli/internal/api"
)

// Run is the entrypoint used by cmd/rc/main.go. Wraps cobra.Execute() so we
// can format final errors based on --json — emitting a structured envelope
// for agents and a friendly stderr message (with actionable hint) for humans.
//
// Keeping this here (rather than in main.go) means the JSON error contract
// lives next to the JSON success contract in output.go.
func Run(version string) int {
	root := NewRootCmd(version)
	err := root.Execute()
	if err == nil {
		return 0
	}
	// SilentExitError means the command already wrote its output; skip the
	// error envelope so there is only one JSON document on stdout.
	var silent *SilentExitError
	if errors.As(err, &silent) {
		return silent.Code
	}
	jsonMode, _ := root.PersistentFlags().GetBool("json")
	if jsonMode {
		writeJSONError(os.Stderr, err)
	} else {
		fmt.Fprintln(os.Stderr, err)
		if hint := hintFor(err); hint != "" {
			fmt.Fprintln(os.Stderr, "Hint: "+hint)
		}
	}
	return ExitCodeFor(err)
}

// hintFor surfaces the actionable next-step text for an error. Pulled out so
// both human stderr and the JSON envelope use the same source of truth.
func hintFor(err error) string {
	var apiErr *api.Error
	if errors.As(err, &apiErr) {
		return apiErr.Hint()
	}
	if errors.Is(err, ErrNotAuthenticated) {
		return "Run `rc login` or set RC_API_KEY."
	}
	return ""
}

// errorEnvelope mirrors the API error shape so agents can use the same parser
// for transport errors and CLI errors. fields:
//
//   - type: stable identifier (resource_missing, unauthorized, usage_error, …)
//   - message: human-readable
//   - exit_code: stable mapping from runtime.go:ExitCodeFor
//   - request_id, doc_url: present only for API-origin errors
type errorEnvelope struct {
	Type              string `json:"type"`
	Message           string `json:"message"`
	ExitCode          int    `json:"exit_code"`
	RequestID         string `json:"request_id,omitempty"`
	DocURL            string `json:"doc_url,omitempty"`
	Hint              string `json:"hint,omitempty"`
	RetryAfterSeconds int    `json:"retry_after_seconds,omitempty"`
}

func writeJSONError(w io.Writer, err error) {
	env := errorEnvelope{
		Message:  err.Error(),
		ExitCode: ExitCodeFor(err),
		Type:     "cli_error",
		Hint:     hintFor(err),
	}
	var apiErr *api.Error
	if errors.As(err, &apiErr) {
		env.Type = apiErr.Type
		env.Message = apiErr.Message
		env.RequestID = apiErr.RequestID
		env.DocURL = apiErr.DocURL
		env.RetryAfterSeconds = apiErr.RetryAfterSeconds
	} else if errors.Is(err, ErrNotAuthenticated) {
		env.Type = "unauthorized"
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(map[string]any{
		"error":          env,
		"schema_version": 1,
	})
}
