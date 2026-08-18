package cli

import (
	"errors"
	"fmt"
	"testing"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/output"
)

// ExitCodeFor is the stable contract agents and scripts branch on. Lock the
// whole matrix, not just the usage-error case.
func TestExitCodeFor_Matrix(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"generic", errors.New("boom"), 1},
		{"bad-format", output.ErrBadFormat, 2},
		{"usage", usageError{errors.New("bad flag")}, 2},
		{"wrapped-usage", fmt.Errorf("outer: %w", usageError{errors.New("bad flag")}), 2},
		{"not-authenticated", ErrNotAuthenticated, 4},
		{"api-unauthorized", &api.APIError{Type: "unauthorized"}, 4},
		{"api-auth-error", &api.APIError{Type: "authentication_error"}, 4},
		{"api-forbidden", &api.APIError{Type: "authorization_error"}, 4},
		{"api-resource-missing", &api.APIError{Type: "resource_missing"}, 5},
		{"api-rate-limit-legacy", &api.APIError{Type: "rate_limit_exceeded"}, 6},
		{"api-rate-limit-spec", &api.APIError{Type: "rate_limit_error"}, 6},
		{"api-wrapped-resource-missing", fmt.Errorf("fetch: %w", &api.APIError{Type: "resource_missing"}), 5},
		{"api-other", &api.APIError{Type: "http_error"}, 1},
	}
	for _, tc := range cases {
		if got := ExitCodeFor(tc.err); got != tc.want {
			t.Errorf("%s: ExitCodeFor = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestSilentExitError_HasEmptyMessage(t *testing.T) {
	if got := (&SilentExitError{Code: 7}).Error(); got != "" {
		t.Errorf("SilentExitError.Error() = %q, want empty (Run uses the code directly)", got)
	}
}
