package api

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrorPredicates(t *testing.T) {
	cases := []struct {
		name string
		pred func(error) bool
		typ  string
	}{
		{"already_exists", IsAlreadyExists, "resource_already_exists"},
		{"not_found", IsNotFound, "resource_missing"},
		{"parameter_error", IsParameterError, "parameter_error"},
		{"rate_limited", IsRateLimited, "rate_limit_error"},
		{"unauthorized", IsUnauthorized, "authentication_error"},
		{"server_error", IsServerError, "server_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			match := &APIError{Type: tc.typ}
			if !tc.pred(match) {
				t.Errorf("should match type %q", tc.typ)
			}
			if !tc.pred(fmt.Errorf("wrap: %w", match)) {
				t.Error("should match through a wrapped error")
			}
			if tc.pred(&APIError{Type: "something_else"}) {
				t.Error("should not match an unrelated type")
			}
			if tc.pred(errors.New("plain")) {
				t.Error("should not match a non-APIError")
			}
		})
	}
}

// parseError synthesizes these strings for a bodyless 4xx, so the predicates
// must accept them alongside the spec types.
func TestRateLimitedAndUnauthorizedAcceptLegacyFallbackTypes(t *testing.T) {
	if !IsRateLimited(&APIError{Type: "rate_limit_exceeded"}) {
		t.Error("IsRateLimited should accept the legacy rate_limit_exceeded fallback")
	}
	if !IsUnauthorized(&APIError{Type: "unauthorized"}) {
		t.Error("IsUnauthorized should accept the legacy unauthorized fallback")
	}
}
