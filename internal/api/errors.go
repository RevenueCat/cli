package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
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
	switch e.Type {
	case "unauthorized", "authentication_error":
		return "Your API key may be revoked or expired. Run `rc login` again, or set RC_API_KEY."
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
