package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Error is the typed error returned for all non-2xx API responses.
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
type Error struct {
	Status    int    `json:"-"`
	Type      string `json:"type"`
	Message   string `json:"message"`
	DocURL    string `json:"doc_url,omitempty"`
	Retryable bool   `json:"retryable,omitempty"`
	RequestID string `json:"-"` // not in body; read from X-Request-Id
}

func (e *Error) Error() string {
	if e.Type != "" {
		return fmt.Sprintf("%s: %s", e.Type, e.Message)
	}
	return fmt.Sprintf("http %d: %s", e.Status, e.Message)
}

func parseError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	e := &Error{Status: resp.StatusCode, RequestID: resp.Header.Get("X-Request-Id")}
	if json.Unmarshal(body, e) != nil || (e.Type == "" && e.Message == "") {
		e.Message = string(body)
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
	return e
}
