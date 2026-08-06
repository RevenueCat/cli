package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/revenuecat/cli/internal/cli"
)

// runAuthCmd is runCmdInConfigDir but keeps RC_BASE_URL pointed at a test
// server, so login can validate a key against a stub. The keyring is mocked
// process-wide (see TestMain), so credentials never touch the real keychain.
func runAuthCmd(t *testing.T, configDir, baseURL string, args ...string) (string, string, error) {
	t.Helper()
	t.Setenv("RC_CONFIG_DIR", configDir)
	t.Setenv("RC_API_KEY", "")
	t.Setenv("RC_PROJECT_ID", "")
	t.Setenv("RC_PROFILE", "")
	t.Setenv("RC_BASE_URL", baseURL)

	var out, errb bytes.Buffer
	root := cli.NewRootCmd("test")
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return out.String(), errb.String(), err
}

func statusAuthenticated(t *testing.T, dir, baseURL string) bool {
	t.Helper()
	stdout, _, err := runAuthCmd(t, dir, baseURL, "auth", "status", "--json", "--no-input")
	if err != nil {
		t.Fatalf("status failed: %v", err)
	}
	var st struct {
		Data struct {
			Authenticated bool   `json:"authenticated"`
			Method        string `json:"method"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &st); err != nil {
		t.Fatalf("status not JSON: %v\n%s", err, stdout)
	}
	if st.Data.Authenticated && st.Data.Method != "api_key" {
		t.Errorf("authenticated via api key should report method=api_key, got %q", st.Data.Method)
	}
	return st.Data.Authenticated
}

// A valid API key is validated against the API, then persisted so later
// commands (here, status) see an authenticated session.
func TestAuthLogin_APIKeyValidatesAndPersists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "projects") {
			t.Errorf("unexpected validation path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk_good" {
			t.Errorf("Authorization = %q", got)
		}
		w.Write([]byte(`{"items":[],"next_page":""}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	if _, _, err := runAuthCmd(t, dir, srv.URL, "auth", "login", "--api-key", "sk_good", "--json", "--no-input"); err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if !statusAuthenticated(t, dir, srv.URL) {
		t.Fatal("a successful login should persist credentials")
	}
}

// A rejected key fails fast with a clear error and must not be written to the
// profile.
func TestAuthLogin_BadAPIKeyFailsAndDoesNotPersist(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"type":"unauthorized","message":"bad key"}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	_, _, err := runAuthCmd(t, dir, srv.URL, "auth", "login", "--api-key", "sk_bad", "--no-input")
	if err == nil || !strings.Contains(err.Error(), "didn't work") {
		t.Fatalf("want a rejected-key error, got %v", err)
	}
	if statusAuthenticated(t, dir, srv.URL) {
		t.Fatal("a failed login must not persist credentials")
	}
}

func TestAuthLogout_ClearsCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"items":[],"next_page":""}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	if _, _, err := runAuthCmd(t, dir, srv.URL, "auth", "login", "--api-key", "sk_good", "--json", "--no-input"); err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if _, _, err := runAuthCmd(t, dir, srv.URL, "auth", "logout", "--json", "--no-input"); err != nil {
		t.Fatalf("logout failed: %v", err)
	}
	if statusAuthenticated(t, dir, srv.URL) {
		t.Fatal("logout should clear the stored credentials")
	}
}
