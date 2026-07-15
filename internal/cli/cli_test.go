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
	"strings"
	"testing"

	"github.com/revenuecat/cli/internal/cli"
)

// runCmd executes the root cobra tree with explicit args + a fresh background
// context, returning captured stdout, stderr, and any error.
func runCmd(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	// Route config to a tempdir so tests don't read/write real profiles.
	t.Setenv("RC_CONFIG_DIR", t.TempDir())
	t.Setenv("RC_API_KEY", "")
	t.Setenv("RC_PROJECT_ID", "")
	t.Setenv("RC_BASE_URL", "")
	t.Setenv("RC_PROFILE", "")

	var out, errb bytes.Buffer
	root := cli.NewRootCmd("test")
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetArgs(args)
	err = root.ExecuteContext(context.Background())
	return out.String(), errb.String(), err
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

func TestConfigureAppleSchema_ExposesNonInteractiveInputs(t *testing.T) {
	t.Setenv("RC_APPLE_PASSWORD", "must-not-appear-in-schema")
	out, _, err := runCmd(t, "schema", "apps", "configure-apple", "--json")
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
		"vendor-number": false, "yes": false, "no-input": false,
	}
	for _, flag := range got.Data.Flags {
		if _, ok := want[flag.Name]; ok {
			want[flag.Name] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("schema for `apps configure-apple` missing flag --%s", name)
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
	// --no-input it must fail without hanging.
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
	if got.Data.Source != "RevenueCat/ai-toolkit" || !strings.Contains(got.Data.InstallCommand, "npx skills add") || got.Data.DocsURL == "" {
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
