// Package cli integration tests cover the cobra wiring + the dual-mode
// contract that every command must honor:
//
//   - --json emits a stable envelope on stdout, nothing on stderr.
//   - Status helpers (Success/Info/Warn) never run in --json mode.
//   - --no-input fails cleanly on missing prompts rather than hanging.
//   - rc commands --json and rc schema <cmd> stay discoverable for agents.
//
// These guard the agent-friendliness story — if they break, an agent driving
// the CLI breaks too.
package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/cli"
	"github.com/revenuecat/cli/internal/config"
)

// runCmd executes the root cobra tree with explicit args + a fresh background
// context, returning captured stdout, stderr, and any error.
func runCmd(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	return runCmdInConfigDir(t, t.TempDir(), args...)
}

func runCmdInConfigDir(t *testing.T, configDir string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	// Route config to a tempdir so tests don't read/write real profiles.
	t.Setenv("RC_CONFIG_DIR", configDir)
	t.Setenv("RC_API_KEY", "")
	t.Setenv("RC_PROJECT_ID", "")
	t.Setenv("RC_BASE_URL", "")
	t.Setenv("RC_PROFILE", "")
	t.Setenv("RC_PASSWORD", "")

	var out, errb bytes.Buffer
	root := cli.NewRootCmd("test")
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetArgs(args)
	err = root.ExecuteContext(context.Background())
	return out.String(), errb.String(), err
}

func TestAuthSignup_AgentFlowStoresDurableOAuthWithoutLeakingTemporaryCredentials(t *testing.T) {
	const temporaryToken = "temporary-login-token-must-not-leak"
	var generatedPassword string
	var requests []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/oauth2/token" && r.Header.Get("X-Requested-With") != "XMLHttpRequest" {
			t.Errorf("%s missing required X-Requested-With header", r.URL.Path)
		}
		switch r.URL.Path {
		case "/v1/developers/provision-account":
			var body struct {
				Email                 string `json:"email"`
				Name                  string `json:"name"`
				Password              string `json:"password"`
				MarketingEmailEnabled bool   `json:"marketing_email_enabled"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Email != "dev@example.com" || body.Name != "Example Developer" || !body.MarketingEmailEnabled {
				t.Fatalf("unexpected provision body: %+v", body)
			}
			generatedPassword = body.Password
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{}`)
		case "/v1/developers/login":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["password"] == "" || body["password"] != generatedPassword {
				t.Fatal("login did not reuse the generated password")
			}
			_, _ = fmt.Fprintf(w, `{"authentication_token":%q}`, temporaryToken)
		case "/v1/developers/me/oauth-authorize":
			if got := r.URL.Query().Get("scope"); got != api.DefaultOAuthScope {
				t.Errorf("scope = %q, want %q", got, api.DefaultOAuthScope)
			}
			if r.Header.Get("Authorization") != "Bearer "+temporaryToken {
				t.Fatalf("unexpected authorization header: %q", r.Header.Get("Authorization"))
			}
			redirectURI := r.URL.Query().Get("redirect_uri")
			redirect, err := url.Parse(redirectURI)
			if err != nil {
				t.Fatal(err)
			}
			query := redirect.Query()
			query.Set("code", "oauth-code")
			query.Set("state", r.URL.Query().Get("state"))
			redirect.RawQuery = query.Encode()
			_, _ = fmt.Fprintf(w, `{"redirect_uri":%q}`, redirect.String())
		case "/oauth2/token":
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code_verifier") == "" {
				t.Fatalf("unexpected token form: %v", r.Form)
			}
			_, _ = io.WriteString(w, `{"access_token":"durable-access","refresh_token":"durable-refresh","expires_in":3600,"token_type":"Bearer"}`)
		case "/v1/developers/logout":
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{}`)
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	t.Setenv("RC_OAUTH_BASE_URL", server.URL)
	t.Setenv("RC_OAUTH_CLIENT_ID", "cli-test")

	configDir := t.TempDir()
	out, errb, err := runCmdInConfigDir(t, configDir, "auth", "signup",
		"--email", "dev@example.com",
		"--name", "Example Developer",
		"--accept-terms", "--marketing-emails", "--no-input", "--json")
	if err != nil {
		t.Fatalf("execute: %v\nstderr: %s", err, errb)
	}
	if generatedPassword == "" {
		t.Fatal("signup did not generate a password")
	}
	profile, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	if profile.AccessToken != "durable-access" || profile.RefreshToken != "durable-refresh" || profile.TokenType != "oauth" || profile.AccountEmail != "dev@example.com" || profile.AccountName != "Example Developer" {
		t.Fatalf("durable OAuth credentials were not saved: %+v", profile)
	}
	profileInfo, err := os.Stat(configDir + "/default.json")
	if err != nil {
		t.Fatal(err)
	}
	if profileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("profile permissions = %o, want 600", profileInfo.Mode().Perm())
	}
	combinedOutput := out + errb
	for _, secret := range []string{generatedPassword, temporaryToken, "durable-access", "durable-refresh"} {
		if strings.Contains(combinedOutput, secret) {
			t.Fatalf("signup output leaked credential %q", secret)
		}
	}
	wantRequests := []string{
		"POST /v1/developers/provision-account",
		"POST /v1/developers/login",
		"POST /v1/developers/me/oauth-authorize",
		"POST /oauth2/token",
		"POST /v1/developers/logout",
	}
	if strings.Join(requests, "\n") != strings.Join(wantRequests, "\n") {
		t.Fatalf("requests = %v, want %v", requests, wantRequests)
	}
	for _, want := range []string{
		`"account_created": true`,
		`"authenticated": true`,
		`"method": "oauth"`,
		`"password_mode": "generated"`,
		`"dashboard_password_action": "use_password_reset_if_needed"`,
		`"agent": "Use the RevenueCat AI Toolkit to create and configure the project, apps, products, entitlements, and offerings."`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("signup JSON missing %s:\n%s", want, out)
		}
	}

	requests = nil
	generatedPassword = ""
	out, errb, err = runCmd(t, "auth", "signup",
		"--email", "dev@example.com",
		"--name", "Example Developer",
		"--password", "user-chosen-password",
		"--accept-terms", "--marketing-emails", "--no-input")
	if err != nil {
		t.Fatalf("pretty execute: %v\nstderr: %s", err, errb)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("pretty signup dumped structured output:\n%s", out)
	}
	for _, want := range []string{
		"Creating your RevenueCat account",
		"Starting a temporary secure login",
		"Authorizing renewable CLI access",
		"Exchanging the temporary session for OAuth tokens",
		"Account created and logged in",
	} {
		if !strings.Contains(errb, want) {
			t.Errorf("pretty signup progress missing %q:\n%s", want, errb)
		}
	}
}

func TestAuthSignup_NoInputListsEveryRequiredFlag(t *testing.T) {
	_, _, err := runCmd(t, "auth", "signup", "--no-input", "--json")
	if err == nil {
		t.Fatal("expected missing input error")
	}
	for _, flag := range []string{"--email", "--name", "--accept-terms"} {
		if !strings.Contains(err.Error(), flag) {
			t.Errorf("error missing %s: %v", flag, err)
		}
	}
}

func TestAuthSignupHelpExplainsCredentialHandling(t *testing.T) {
	out, _, err := runCmd(t, "auth", "signup", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"create a password or generate a strong random one",
		"app.revenuecat.com internet password in the local",
		"cannot add credentials",
		"never printed",
		"Renewable OAuth tokens",
		"saved in the active local profile",
		"personal/display name",
		"--generate-password --save-password",
		"--accept-terms",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("signup help missing %q:\n%s", want, out)
		}
	}
}

func TestAuthStatusAndLogoutOnlyRenderStructuredDataWithJSON(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("RC_CONFIG_DIR", configDir)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects" {
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","items":[{"id":"proj_test","name":"Test","created_at":1,"object":"project"}]}`)
	}))
	t.Cleanup(server.Close)
	if err := config.Save("", &config.Config{TokenType: "oauth", AccessToken: "access", AccountEmail: "dev@example.com", AccountName: "Example Developer", ProjectID: "proj_test", BaseURL: server.URL}); err != nil {
		t.Fatal(err)
	}

	out, errb, err := runCmdInConfigDir(t, configDir, "auth", "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("human status dumped structured output:\n%s", out)
	}
	if !strings.Contains(errb, "Logged in as Example Developer <dev@example.com> (profile: default)") {
		t.Fatalf("human status missing summary: %s", errb)
	}

	out, errb, err = runCmdInConfigDir(t, configDir, "auth", "whoami")
	if err != nil {
		t.Fatalf("auth whoami: %v", err)
	}
	if strings.TrimSpace(out) != "" || !strings.Contains(errb, "dev@example.com") {
		t.Fatalf("auth whoami did not use status output: stdout=%s stderr=%s", out, errb)
	}

	out, errb, err = runCmdInConfigDir(t, configDir, "auth", "status", "--json")
	if err != nil {
		t.Fatalf("JSON status: %v", err)
	}
	if errb != "" {
		t.Fatalf("JSON status wrote stderr: %s", errb)
	}
	for _, want := range []string{`"authenticated": true`, `"method": "oauth"`, `"account_email": "dev@example.com"`, `"account_name": "Example Developer"`, `"project_id": "proj_test"`, `"project_status": "valid"`} {
		if !strings.Contains(out, want) {
			t.Errorf("JSON status missing %s:\n%s", want, out)
		}
	}

	out, errb, err = runCmdInConfigDir(t, configDir, "auth", "logout")
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("human logout dumped structured output:\n%s", out)
	}
	if !strings.Contains(errb, "Logged out (profile: default)") {
		t.Fatalf("human logout missing summary: %s", errb)
	}
	profile, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	if profile.AccountEmail != "" || profile.AccountName != "" {
		t.Fatalf("logout retained cached identity: %+v", profile)
	}
}

func TestAuthStatusFlagsDanglingProject(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("RC_CONFIG_DIR", configDir)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","items":[]}`)
	}))
	t.Cleanup(server.Close)
	if err := config.Save("", &config.Config{APIKey: "sk_test", ProjectID: "proj_gone", BaseURL: server.URL}); err != nil {
		t.Fatal(err)
	}

	out, _, err := runCmdInConfigDir(t, configDir, "auth", "status", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"project_status": "not_found"`) {
		t.Fatalf("unexpected status: %s", out)
	}
}

func TestCommandsJSON_AgentDiscovery(t *testing.T) {
	out, errb, err := runCmd(t, "commands", "--json")
	if err != nil {
		t.Fatalf("execute: %v\nstderr:%s", err, errb)
	}
	var got struct {
		Data          map[string]any `json:"data"`
		SchemaVersion int            `json:"schema_version"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if got.SchemaVersion != 1 {
		t.Errorf("want schema_version=1, got %d", got.SchemaVersion)
	}
	// Top of the tree is the root command.
	if got.Data["name"] != "rc" {
		t.Errorf("want top-level name=rc, got %v", got.Data["name"])
	}
	// Spot-check a few critical subcommands exist for agents to find.
	tree := string(out)
	for _, want := range []string{`"auth"`, `"customer"`, `"entitlements"`, `"schema"`, `"projects"`} {
		if !strings.Contains(tree, want) {
			t.Errorf("expected %s in command tree", want)
		}
	}
}

func TestSchemaCommand_ReturnsFlagSchema(t *testing.T) {
	out, _, err := runCmd(t, "schema", "auth", "login", "--json")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var got struct {
		Data struct {
			Name  string `json:"name"`
			Flags []struct {
				Name        string `json:"name"`
				Type        string `json:"type"`
				Description string `json:"description"`
			} `json:"flags"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if got.Data.Name != "login" {
		t.Errorf("want name=login, got %q", got.Data.Name)
	}
	// auth login must expose api-key + project-id flags so agents can drive it non-interactively.
	want := map[string]bool{"api-key": false, "project-id": false}
	for _, f := range got.Data.Flags {
		if _, ok := want[f.Name]; ok {
			want[f.Name] = true
		}
	}
	for k, seen := range want {
		if !seen {
			t.Errorf("schema for `login` missing flag --%s", k)
		}
	}
}

func TestStorePlanSchema_ExplainsAgentHandoffAndStdin(t *testing.T) {
	out, _, err := runCmd(t, "schema", "products", "store", "plan", "--json")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"name": "file"`, `"name": "input-format"`, "returned plan ID", "--file -"} {
		if !strings.Contains(out, want) {
			t.Errorf("store plan schema missing %q\n%s", want, out)
		}
	}
}

func TestAppsAppleSetupSchema_ExposesNonInteractiveInputs(t *testing.T) {
	t.Setenv("RC_APPLE_PASSWORD", "must-not-appear-in-schema")
	out, _, err := runCmd(t, "schema", "apps", "apple", "setup", "--json")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var got struct {
		Data struct {
			Flags []struct {
				Name string `json:"name"`
			} `json:"flags"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if strings.Contains(out, "must-not-appear-in-schema") {
		t.Fatal("schema leaked RC_APPLE_PASSWORD")
	}
	want := map[string]bool{
		"apple-id": false, "apple-password": false, "verification-code": false,
		"sms": false, "phone-number": false, "team-id": false,
		"vendor-number": false, "force": false, "yes": false, "no-input": false,
	}
	for _, flag := range got.Data.Flags {
		if _, ok := want[flag.Name]; ok {
			want[flag.Name] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("schema for `apps apple setup` missing flag --%s", name)
		}
	}
}

func TestAppsAppleSetupHelp_ExplainsCredentialFlow(t *testing.T) {
	out, _, err := runCmd(t, "apps", "apple", "setup", "--help")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for _, want := range []string{
		"sent directly to Apple",
		"sent to RevenueCat or stored by rc",
		"only in memory for the duration of this command",
		"uploaded directly to RevenueCat",
		"never saved locally or printed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q:\n%s", want, out)
		}
	}
}

func TestAppsAppleCheckHelp_IsExplicitlyReadOnly(t *testing.T) {
	out, _, err := runCmd(t, "apps", "apple", "check", "--help")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for _, want := range []string{
		"read-only requests",
		"No Apple keys will be created",
		"no RevenueCat app will be changed",
		"creates no Apple keys and makes no changes in RevenueCat",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"--force", "--vendor-number", "--dry-run"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("check help unexpectedly includes %s:\n%s", unwanted, out)
		}
	}
}

func TestWhoami_JSON_StableShape(t *testing.T) {
	out, errb, err := runCmd(t, "whoami", "--json")
	if err != nil {
		t.Fatalf("execute: %v\nstderr:%s", err, errb)
	}
	if errb != "" {
		t.Errorf("--json must not write to stderr; got %q", errb)
	}
	var got struct {
		Data struct {
			Profile       string `json:"profile"`
			Authenticated bool   `json:"authenticated"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if got.Data.Profile != "default" {
		t.Errorf("want profile=default in fresh env, got %q", got.Data.Profile)
	}
	if got.Data.Authenticated {
		t.Error("fresh env should not be authenticated")
	}
}

func TestNoInput_FailsCleanlyOnMissingRequired(t *testing.T) {
	// `entitlements create` requires lookup-key. Without --no-input AND with no
	// TTY (test runs piped) the tui form will still try to render; under
	// --no-input it must fail without hanging. The rigorous check that the
	// required-field validation actually fires under --no-input lives in the
	// tui package (TestForm_NoInput_*); this is the end-to-end smoke test.
	t.Setenv("RC_API_KEY", "sk_x") // bypass not-authenticated error
	_, _, err := runCmd(t, "entitlements", "create", "--no-input")
	if err == nil {
		t.Fatal("want error when required input missing under --no-input")
	}
}

func TestUnknownCommand_ReturnsUsageError(t *testing.T) {
	_, _, err := runCmd(t, "definitely-not-a-real-command")
	if err == nil {
		t.Fatal("want error for unknown command")
	}
}

func TestSchema_IncludesPositionalArgs(t *testing.T) {
	out, _, err := runCmd(t, "schema", "entitlements", "attach", "--json")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var got struct {
		Data struct {
			Path string `json:"path"`
			Args []struct {
				Name     string `json:"name"`
				Required bool   `json:"required"`
				Variadic bool   `json:"variadic"`
			} `json:"args"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if got.Data.Path != "rc entitlements attach" {
		t.Errorf("want path 'rc entitlements attach', got %q", got.Data.Path)
	}
	if len(got.Data.Args) < 2 {
		t.Fatalf("want at least 2 positional args (id, product-id), got %d", len(got.Data.Args))
	}
	if got.Data.Args[0].Name != "id" || !got.Data.Args[0].Required {
		t.Errorf("first arg should be required <id>, got %+v", got.Data.Args[0])
	}
	// Last arg is [product-id...] — optional + variadic.
	last := got.Data.Args[len(got.Data.Args)-1]
	if !last.Variadic {
		t.Errorf("last arg should be variadic, got %+v", last)
	}
}

func TestSchema_IncludesAliases(t *testing.T) {
	out, errb, err := runCmd(t, "schema", "customer", "--json")
	if err != nil {
		t.Fatalf("execute: %v\nstderr: %s\nstdout: %s", err, errb, out)
	}
	var got struct {
		Data struct {
			Name        string   `json:"name"`
			Aliases     []string `json:"aliases"`
			Subcommands []struct {
				Name string `json:"name"`
			} `json:"subcommands"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\nstdout: %s", err, out)
	}
	if got.Data.Name != "customer" {
		t.Fatalf("wrong command resolved: name=%q stdout=%s", got.Data.Name, out)
	}
	if !contains(got.Data.Aliases, "customers") {
		t.Errorf("want 'customers' in aliases, got %v", got.Data.Aliases)
	}
	subNames := make([]string, len(got.Data.Subcommands))
	for i, s := range got.Data.Subcommands {
		subNames[i] = s.Name
	}
	if !contains(subNames, "grant") || !contains(subNames, "revoke") {
		t.Errorf("subcommands missing core verbs: %v", subNames)
	}
}

func TestVersion_JSONShape(t *testing.T) {
	out, _, err := runCmd(t, "version", "--json")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var got struct {
		Data struct {
			Version string `json:"version"`
		} `json:"data"`
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if got.Data.Version != "test" {
		t.Errorf("want version=test, got %q", got.Data.Version)
	}
	if got.SchemaVersion != 1 {
		t.Errorf("want schema_version=1, got %d", got.SchemaVersion)
	}
}

func TestSkills_JSONPointsToOfficialToolkit(t *testing.T) {
	out, _, err := runCmd(t, "skills", "--json")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var got struct {
		Data struct {
			Source         string `json:"source"`
			InstallCommand string `json:"install_command"`
			DocsURL        string `json:"docs_url"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if got.Data.Source != "RevenueCat/ai-toolkit" || got.Data.InstallCommand != "rc skills install" || got.Data.DocsURL == "" {
		t.Fatalf("unexpected toolkit guidance: %+v", got.Data)
	}
}

func TestSkillsInstallSchemaExposesStandardInstallerOptions(t *testing.T) {
	out, _, err := runCmd(t, "schema", "skills", "install", "--json")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for _, want := range []string{`"name": "agent"`, `"name": "skill"`, `"name": "global"`, `"name": "copy"`, "RevenueCat/ai-toolkit"} {
		if !strings.Contains(out, want) {
			t.Errorf("skills install schema missing %q", want)
		}
	}
}

func TestSchema_IncludesCapabilities(t *testing.T) {
	out, _, err := runCmd(t, "schema", "products", "--json")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var got struct {
		Data struct {
			Capabilities []string `json:"capabilities"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	for _, want := range []string{"list", "show", "create", "delete", "archive"} {
		if !contains(got.Data.Capabilities, want) {
			t.Errorf("want capability %q in products schema, got %v", want, got.Data.Capabilities)
		}
	}
}

func TestCommandTree_IncludesNewCommands(t *testing.T) {
	out, _, err := runCmd(t, "commands", "--json")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for _, want := range []string{`"skills"`, `"api"`, `"packages"`} {
		if !strings.Contains(out, want) {
			t.Errorf("want %s in command tree", want)
		}
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// Usage errors (bad flags, unknown command) get the conventional exit code 2,
// distinct from a runtime failure (1). Guards the FlagErrorFunc sentinel and
// the cobra-message match in ExitCodeFor.
func TestExitCode_UsageErrorsAre2(t *testing.T) {
	_, _, err := runCmd(t, "definitely-not-a-real-command")
	if got := cli.ExitCodeFor(err); got != 2 {
		t.Errorf("unknown command should exit 2, got %d (err: %v)", got, err)
	}
	_, _, err = runCmd(t, "version", "--definitely-not-a-flag")
	if got := cli.ExitCodeFor(err); got != 2 {
		t.Errorf("unknown flag should exit 2, got %d (err: %v)", got, err)
	}
}

func TestUnknownSubcommand_ErrorsAcrossGroups(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"non-runnable group", []string{"apps", "frobnicate"}},
		{"another non-runnable group", []string{"auth", "frobnicate"}},
		{"runnable guided group", []string{"paywalls", "create"}},
		{"runnable list group", []string{"packages", "frobnicate"}},
		{"nested group", []string{"apps", "apple", "frobnicate"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, _, err := runCmd(t, tc.args...)
			if err == nil {
				t.Fatalf("want error for unknown subcommand %v, got nil (stdout: %s)", tc.args, out)
			}
			if got := cli.ExitCodeFor(err); got != 2 {
				t.Errorf("unknown subcommand should exit 2, got %d (err: %v)", got, err)
			}
			if !strings.HasPrefix(err.Error(), "unknown command") {
				t.Errorf("want an unknown-command message, got %q", err.Error())
			}
		})
	}
}

func TestUnknownSubcommand_PreservesBareGroupAndValidSubcommand(t *testing.T) {
	out, _, err := runCmd(t, "apps")
	if err != nil {
		t.Fatalf("bare group should show help, not error: %v", err)
	}
	if !strings.Contains(out, "Available Commands:") {
		t.Errorf("bare group should render help with its subcommands, got:\n%s", out)
	}

	// A valid subcommand isn't guarded: it may fail on missing auth (exit 4),
	// but never as unknown-command (exit 2).
	_, _, err = runCmd(t, "apps", "list", "--no-input")
	if err != nil && strings.HasPrefix(err.Error(), "unknown command") {
		t.Errorf("valid subcommand misrouted as unknown: %v", err)
	}
}

func TestGroupHelpHidesBareUseLine(t *testing.T) {
	out, _, err := runCmd(t, "apps", "--help")
	if err != nil {
		t.Fatalf("apps --help: %v", err)
	}
	if !strings.Contains(out, "rc apps [command]") {
		t.Fatalf("group usage should offer the subcommand form:\n%s", out)
	}
	if strings.Contains(out, "rc apps [flags]") {
		t.Errorf("help-only group must not render a runnable useline:\n%s", out)
	}
}

// The guard makes groups cobra-runnable, but that must not leak into the agent
// discovery surface: a group stays runnable:false and never lists its own name
// as a capability.
func TestUnknownSubcommand_KeepsGroupsOutOfDiscoverySurface(t *testing.T) {
	out, _, err := runCmd(t, "commands", "--json")
	if err != nil {
		t.Fatalf("commands --json: %v", err)
	}
	type node struct {
		Name         string   `json:"name"`
		Runnable     bool     `json:"runnable"`
		Capabilities []string `json:"capabilities"`
		Commands     []node   `json:"commands"`
	}
	var got struct {
		Data node `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	var apps *node
	for i := range got.Data.Commands {
		if got.Data.Commands[i].Name == "apps" {
			apps = &got.Data.Commands[i]
		}
	}
	if apps == nil {
		t.Fatal("apps group missing from commands tree")
	}
	if apps.Runnable {
		t.Error("pure group apps should report runnable:false in the discovery surface")
	}
	if contains(apps.Capabilities, "apps") {
		t.Errorf("pure group apps must not advertise its own name as a capability, got %v", apps.Capabilities)
	}
}

// Near-misses carry cobra's did-you-mean so an agent can self-correct.
func TestUnknownSubcommand_SuggestsNearMiss(t *testing.T) {
	_, _, err := runCmd(t, "apps", "lst")
	if err == nil {
		t.Fatal("want error for unknown subcommand")
	}
	if !strings.Contains(err.Error(), `did you mean "list"?`) {
		t.Errorf("want a did-you-mean suggestion for 'lst', got %q", err.Error())
	}
}
