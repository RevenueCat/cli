package api_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

func TestMissingScope(t *testing.T) {
	cases := map[string]string{
		"You are not authorized":                                     "",
		"missing required scope: `project_configuration:read_write`": "project_configuration:read_write",
		"insufficient scope 'products:read_write'":                   "products:read_write",
		"token requires the *:*:read_write scope":                    "*:*:read_write",
		"resource not found":                                         "",
	}
	for msg, want := range cases {
		if got := api.MissingScope(msg); got != want {
			t.Errorf("MissingScope(%q) = %q, want %q", msg, got, want)
		}
	}
}

func TestAPIError_ScopeHintNamesEnv(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"type":"unauthorized","message":"missing required scope: 'products:read_write'"}`))
	}))
	defer srv.Close()

	c := api.NewClient(api.Options{APIKey: "sk_env", BaseURL: srv.URL, CredentialSource: "env"})
	_, err := c.Projects.List(context.Background())
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want APIError, got %v", err)
	}
	hint := apiErr.Hint()
	if !strings.Contains(hint, "products:read_write") {
		t.Errorf("hint should name the missing scope, got %q", hint)
	}
	if !strings.Contains(hint, "RC_API_KEY") {
		t.Errorf("hint should point at RC_API_KEY for an env credential, got %q", hint)
	}
}

func TestAPIError_UnauthorizedHintMentionsLogin(t *testing.T) {
	e := &api.APIError{Status: 401, Type: "unauthorized", Message: "bad key"}
	if hint := e.Hint(); !strings.Contains(hint, "rc login") {
		t.Errorf("hint should mention rc login, got %q", hint)
	}
}

func TestStoreStatePlanFeatureGateHint(t *testing.T) {
	const gateMessage = "Product store state plans are currently a limited beta feature and are not available for this project."
	for _, tc := range []struct {
		name, source, message string
		status                int
		wantAction            string
	}{
		{"flag key gated", "flag", gateMessage, 403, "Omit `--api-key`"},
		{"environment key gated", "env", gateMessage, 403, "Unset RC_API_KEY"},
		{"profile key gated", "profile", gateMessage, 403, "Run `rc login`"},
		{"OAuth gated", "oauth", gateMessage, 403, ""},
		{"other authorization error", "env", "You do not have permission to perform this action.", 403, ""},
		{"other status", "env", gateMessage, 400, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := &api.APIError{Status: tc.status, Type: "authorization_error", Message: tc.message, CredentialSource: tc.source}
			hint := e.Hint()
			if tc.wantAction == "" && hint != "" {
				t.Errorf("unexpected hint: %q", hint)
			}
			if tc.wantAction != "" && (!strings.Contains(hint, tc.wantAction) || !strings.Contains(hint, "store-state plan access")) {
				t.Errorf("hint = %q, want action %q and store-state plan access", hint, tc.wantAction)
			}
		})
	}
}
