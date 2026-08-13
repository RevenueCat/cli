package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// APIError is the typed error returned for all non-2xx API responses.
//
// Wire shape, learned from real fixtures:
//
//	{
//	  "type": "resource_missing",
//	  "message": "Resource not found",
//	  "doc_url": "https://errors.rev.cat/resource-missing",
//	  "retryable": false,
//	  "object": "error"
//	}
//
// CLI exit codes are mapped from Type in internal/cli/runtime.go.
type APIError struct {
	Status            int    `json:"-"`
	Type              string `json:"type"`
	Message           string `json:"message"`
	DocURL            string `json:"doc_url,omitempty"`
	Retryable         bool   `json:"retryable,omitempty"`
	RequestID         string `json:"-"`                     // X-Request-Id header, not in body
	RetryAfterSeconds int    `json:"retry_after,omitempty"` // Retry-After header on 429

	// CredentialSource names where the request's credential came from; set by the client, not the wire.
	CredentialSource string `json:"-"`
}

func (e *APIError) Error() string {
	if e.Type != "" {
		return fmt.Sprintf("%s: %s", e.Type, e.Message)
	}
	return fmt.Sprintf("http %d: %s", e.Status, e.Message)
}

func parseError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	e := &APIError{Status: resp.StatusCode, RequestID: resp.Header.Get("X-Request-Id")}
	if json.Unmarshal(body, e) != nil || (e.Type == "" && e.Message == "") {
		e.Message = fmt.Sprintf("upstream returned a non-JSON HTTP %d response (%s)", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	if e.Type == "" {
		switch resp.StatusCode {
		case 401, 403:
			e.Type = "unauthorized"
		case 404:
			e.Type = "resource_missing"
		case 429:
			e.Type = "rate_limit_exceeded"
		default:
			e.Type = "http_error"
		}
	}
	if resp.StatusCode == 429 {
		if v := resp.Header.Get("Retry-After"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				e.RetryAfterSeconds = n
			}
		}
	}
	return e
}

// Hint returns an actionable next step for the user, given the error type +
// status. Returns empty when nothing useful can be said.
func (e *APIError) Hint() string {
	if scope := MissingScope(e.Message); scope != "" {
		return e.scopeHint(scope)
	}
	switch e.Type {
	case "unauthorized", "authentication_error":
		return e.authHint()
	case "rate_limit_exceeded":
		if e.RetryAfterSeconds > 0 {
			return fmt.Sprintf("Rate limited. Retry after %d seconds.", e.RetryAfterSeconds)
		}
		return "Rate limited. Back off and retry."
	}
	if e.Status >= 500 {
		return "API issue. Retry, or check https://status.revenuecat.com."
	}
	return ""
}

func (e *APIError) credentialSourceNote() string {
	switch e.CredentialSource {
	case "env":
		return " The active credential came from the RC_API_KEY environment variable — check your shell env (e.g. ~/.zshrc), or run `rc login`."
	case "flag":
		return " The active credential came from the --api-key flag; pass a key with the required scope, or run `rc login`."
	case "oauth":
		return " Run `rc login` again to refresh your session."
	default:
		return " Run `rc login` again, or set RC_API_KEY to a key with the required scope."
	}
}

func (e *APIError) authHint() string {
	return "Your credential may be revoked, expired, or missing a required scope." + e.credentialSourceNote()
}

func (e *APIError) scopeHint(scope string) string {
	return fmt.Sprintf("The active credential is missing the %q scope.", scope) + e.credentialSourceNote()
}

// scopeTokenRe matches scope identifiers like "project_configuration:read_write".
var scopeTokenRe = regexp.MustCompile(`[A-Za-z0-9_*]+:[A-Za-z0-9_*]+(?::[A-Za-z0-9_*]+)?`)

// MissingScope extracts the scope named in a permission/scope error message, or "" if none.
func MissingScope(msg string) string {
	if msg == "" || !strings.Contains(strings.ToLower(msg), "scope") {
		return ""
	}
	for _, q := range []byte{'`', '"', '\''} {
		if i := strings.IndexByte(msg, q); i >= 0 {
			if j := strings.IndexByte(msg[i+1:], q); j > 0 {
				if cand := strings.TrimSpace(msg[i+1 : i+1+j]); cand != "" {
					return cand
				}
			}
		}
	}
	if m := scopeTokenRe.FindString(msg); m != "" {
		return m
	}
	return ""
}
