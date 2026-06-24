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
	"os"
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

func TestSkills_List_JSON(t *testing.T) {
	out, _, err := runCmd(t, "skills", "list", "--json")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var got struct {
		Data struct {
			Items []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if len(got.Data.Items) == 0 {
		t.Fatal("want at least one skill, got none")
	}
	// Every item must have a name and description.
	for _, s := range got.Data.Items {
		if s.Name == "" {
			t.Errorf("skill missing name: %+v", s)
		}
		if s.Description == "" {
			t.Errorf("skill %q missing description", s.Name)
		}
	}
	// Check a known skill is always present.
	names := make([]string, len(got.Data.Items))
	for i, s := range got.Data.Items {
		names[i] = s.Name
	}
	for _, want := range []string{"setup-offering", "debug-customer"} {
		if !contains(names, want) {
			t.Errorf("want skill %q in list, got %v", want, names)
		}
	}
}

func TestSkills_Show(t *testing.T) {
	out, _, err := runCmd(t, "skills", "show", "setup-offering")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "rc offerings create") {
		t.Errorf("setup-offering skill should mention 'rc offerings create'; got:\n%s", out)
	}
}

func TestSkills_Show_UnknownName(t *testing.T) {
	_, _, err := runCmd(t, "skills", "show", "definitely-not-a-skill")
	if err == nil {
		t.Fatal("want error for unknown skill name")
	}
}

func TestSkills_Install(t *testing.T) {
	dir := t.TempDir()
	out, _, err := runCmd(t, "skills", "install", "--dir", dir, "--json")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var got struct {
		Data struct {
			Installed []string `json:"installed"`
			Directory string   `json:"directory"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if len(got.Data.Installed) == 0 {
		t.Fatal("want at least one installed file")
	}
	// Every reported file must actually exist on disk.
	for _, f := range got.Data.Installed {
		path := got.Data.Directory + "/" + f
		if _, err := os.Stat(path); err != nil {
			t.Errorf("installed file missing on disk: %s", path)
		}
	}
	// Files must be prefixed rc-.
	for _, f := range got.Data.Installed {
		if !strings.HasPrefix(f, "rc-") {
			t.Errorf("installed filename should start with rc-, got %q", f)
		}
	}
}

func TestSkills_Install_SingleSkill(t *testing.T) {
	dir := t.TempDir()
	_, _, err := runCmd(t, "skills", "install", "setup-offering", "--dir", dir)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("want exactly 1 file, got %d", len(entries))
	}
	if entries[0].Name() != "rc-setup-offering.md" {
		t.Errorf("want rc-setup-offering.md, got %q", entries[0].Name())
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
	for _, want := range []string{`"skills"`, `"api"`, `"packages"`, `"prices"`} {
		if !strings.Contains(out, want) {
			t.Errorf("want %s in command tree", want)
		}
	}
}

func TestSchema_ProductPricesCommands(t *testing.T) {
	out, _, err := runCmd(t, "schema", "products", "prices", "--json")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var got struct {
		Data struct {
			Subcommands []struct {
				Name string `json:"name"`
			} `json:"subcommands"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	names := make([]string, 0, len(got.Data.Subcommands))
	for _, sub := range got.Data.Subcommands {
		names = append(names, sub.Name)
	}
	for _, want := range []string{"list", "add", "update"} {
		if !contains(names, want) {
			t.Errorf("want products prices subcommand %q, got %v", want, names)
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
