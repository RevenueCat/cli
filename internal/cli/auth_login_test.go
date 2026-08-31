package cli_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/revenuecat/cli/internal/cli"
	"github.com/revenuecat/cli/internal/config"
)

func runStatus(t *testing.T, configDir string, args ...string) (string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	root := cli.NewRootCmd("test")
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetArgs(append([]string{"auth", "status"}, args...))
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("auth status failed: %v", err)
	}
	return out.String(), errb.String()
}

// RC_API_KEY is a per-invocation override: it outranks the stored OAuth
// login, and the shadowing is reported via credential_conflict.
func TestAuthStatus_EnvAPIKeyShadowsOAuth(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RC_CONFIG_DIR", dir)
	t.Setenv("RC_PROFILE", "")
	t.Setenv("RC_PROJECT_ID", "")
	t.Setenv("RC_BASE_URL", "")
	t.Setenv("RC_API_KEY", "")
	if err := config.Save("default", &config.Config{TokenType: "oauth", AccessToken: "oauth_at", RefreshToken: "oauth_rt"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RC_API_KEY", "sk_under_scoped")

	stdout, _ := runStatus(t, dir, "--json", "--no-input")
	var st struct {
		Data struct {
			Method           string         `json:"method"`
			CredentialSource string         `json:"credential_source"`
			Conflict         map[string]any `json:"credential_conflict"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &st); err != nil {
		t.Fatalf("status not JSON: %v\n%s", err, stdout)
	}
	if st.Data.Method != "api_key" {
		t.Errorf("method should be api_key, got %q", st.Data.Method)
	}
	if st.Data.CredentialSource != "env" {
		t.Errorf("credential_source should be env, got %q", st.Data.CredentialSource)
	}
	if st.Data.Conflict == nil {
		t.Fatal("expected a credential_conflict field when OAuth + RC_API_KEY coexist")
	}
	if got, _ := st.Data.Conflict["active_source"].(string); got != "env" {
		t.Errorf("conflict active_source should be env, got %q", got)
	}
}

func TestAuthStatus_ConflictWarnsOnStderr(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RC_CONFIG_DIR", dir)
	t.Setenv("RC_PROFILE", "")
	t.Setenv("RC_PROJECT_ID", "")
	t.Setenv("RC_BASE_URL", "")
	t.Setenv("RC_API_KEY", "")
	if err := config.Save("default", &config.Config{TokenType: "oauth", AccessToken: "oauth_at"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RC_API_KEY", "sk_under_scoped")

	_, stderr := runStatus(t, dir, "--no-input")
	if !strings.Contains(stderr, "RC_API_KEY") {
		t.Errorf("stderr should warn about the RC_API_KEY conflict, got %q", stderr)
	}
}

func TestAuthStatus_EnvKeyReportedAsEnvSource(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RC_CONFIG_DIR", dir)
	t.Setenv("RC_PROFILE", "")
	t.Setenv("RC_PROJECT_ID", "")
	t.Setenv("RC_BASE_URL", "")
	t.Setenv("RC_API_KEY", "sk_ci")

	stdout, _ := runStatus(t, dir, "--json", "--no-input")
	var st struct {
		Data struct {
			Method           string         `json:"method"`
			CredentialSource string         `json:"credential_source"`
			Conflict         map[string]any `json:"credential_conflict"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &st); err != nil {
		t.Fatalf("status not JSON: %v\n%s", err, stdout)
	}
	if st.Data.Method != "api_key" || st.Data.CredentialSource != "env" {
		t.Errorf("want api_key/env, got %q/%q", st.Data.Method, st.Data.CredentialSource)
	}
	if st.Data.Conflict != nil {
		t.Errorf("no conflict expected with only RC_API_KEY, got %v", st.Data.Conflict)
	}
}

func TestAuthStatus_ScopesFromJWT(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RC_CONFIG_DIR", dir)
	t.Setenv("RC_PROFILE", "")
	t.Setenv("RC_PROJECT_ID", "")
	t.Setenv("RC_BASE_URL", "")
	t.Setenv("RC_API_KEY", "")

	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"scope":"projects:read_write products:read_write"}`))
	jwt := "aGVhZGVy." + payload + ".c2ln"
	if err := config.Save("default", &config.Config{TokenType: "oauth", AccessToken: jwt}); err != nil {
		t.Fatal(err)
	}

	stdout, _ := runStatus(t, dir, "--scopes", "--json", "--no-input")
	var st struct {
		Data struct {
			Scopes []string `json:"scopes"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &st); err != nil {
		t.Fatalf("status not JSON: %v\n%s", err, stdout)
	}
	if len(st.Data.Scopes) != 2 || st.Data.Scopes[0] != "projects:read_write" {
		t.Errorf("want the two JWT scopes, got %v", st.Data.Scopes)
	}
}

func TestAuthStatus_ScopesUnavailableForOpaqueKey(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RC_CONFIG_DIR", dir)
	t.Setenv("RC_PROFILE", "")
	t.Setenv("RC_PROJECT_ID", "")
	t.Setenv("RC_BASE_URL", "")
	t.Setenv("RC_API_KEY", "")
	if err := config.Save("default", &config.Config{APIKey: "sk_opaque"}); err != nil {
		t.Fatal(err)
	}

	stdout, _ := runStatus(t, dir, "--scopes", "--json", "--no-input")
	var st struct {
		Data struct {
			Scopes          []string `json:"scopes"`
			ScopesAvailable *bool    `json:"scopes_available"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &st); err != nil {
		t.Fatalf("status not JSON: %v\n%s", err, stdout)
	}
	if st.Data.Scopes != nil {
		t.Errorf("scopes should be null for an opaque key, got %v", st.Data.Scopes)
	}
	if st.Data.ScopesAvailable == nil || *st.Data.ScopesAvailable {
		t.Errorf("scopes_available should be false for an opaque key")
	}
}

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
