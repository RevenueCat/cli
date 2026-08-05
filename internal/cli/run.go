package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/output"
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
		fmt.Fprintln(os.Stderr, output.StyleError.Render("✗")+" "+err.Error())
		if hint := hintFor(err); hint != "" {
			fmt.Fprintln(os.Stderr, output.StyleDim.Render("Hint: "+hint))
		}
	}
	return ExitCodeFor(err)
}

type hintError struct {
	err  error
	hint string
}

func (e *hintError) Error() string { return e.err.Error() }
func (e *hintError) Unwrap() error { return e.err }
func (e *hintError) Hint() string  { return e.hint }

func WithHint(err error, hint string) error {
	if err == nil {
		return nil
	}
	return &hintError{err: err, hint: hint}
}

// hintFor surfaces the actionable next-step text for an error. Pulled out so
// both human stderr and the JSON envelope use the same source of truth.
func hintFor(err error) string {
	// Precedence: an attached hint wins over the fallbacks below.
	var hinted *hintError
	if errors.As(err, &hinted) && hinted.hint != "" {
		return hinted.hint
	}
	var apiErr *api.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Hint()
	}
	if errors.Is(err, ErrNotAuthenticated) {
		return "Run `rc login` or set RC_API_KEY."
	}
	msg := err.Error()
	if strings.Contains(msg, "Additional properties are not allowed") &&
		(strings.Contains(msg, "review_information") || strings.Contains(msg, "subscription_group")) {
		return "review_information and subscription_group_localizations belong inside store_state, not at the desired-state top level or under common."
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
	var apiErr *api.APIError
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
