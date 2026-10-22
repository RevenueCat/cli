package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestProfilesRespectProfileFlag(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		expected string
		field    string
	}{
		{"show_flag", []string{"profiles", "show", "--profile", "qa-chosen", "--json"}, "qa-chosen", "profile"},
		{"list_flag", []string{"profiles", "list", "--profile", "qa-chosen", "--json"}, "qa-chosen", "active"},
		{"show_positional_control", []string{"profiles", "show", "qa-chosen", "--json"}, "qa-chosen", "profile"},
		{"show_active_control", []string{"profiles", "show", "--json"}, "qa-base", "profile"},
		{"list_active_control", []string{"profiles", "list", "--json"}, "qa-base", "active"},
		{"positional_overrides_flag_control", []string{"profiles", "show", "qa-base", "--profile", "qa-chosen", "--json"}, "qa-base", "profile"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, project := range map[string]string{"qa-base": "proj_local_base", "qa-chosen": "proj_local_chosen"} {
				raw, _ := json.Marshal(map[string]string{"project_id": project})
				if err := os.WriteFile(filepath.Join(dir, name+".json"), raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(dir, ".active"), []byte("qa-base\n"), 0600); err != nil {
				t.Fatal(err)
			}
			out, stderr, err := runCmdInConfigDir(t, dir, tc.args...)
			if err != nil {
				t.Fatalf("command failed: %v; %s", err, stderr)
			}
			var response struct {
				Data map[string]any `json:"data"`
			}
			if err := json.Unmarshal([]byte(out), &response); err != nil {
				t.Fatal(err)
			}
			if got := response.Data[tc.field]; got != tc.expected {
				t.Errorf("%s = %v, want %s; output: %s", tc.field, got, tc.expected, out)
			}
			if tc.field == "profile" {
				expectedProject := "proj_local_base"
				if tc.expected == "qa-chosen" {
					expectedProject = "proj_local_chosen"
				}
				if got := response.Data["project_id"]; got != expectedProject {
					t.Errorf("project_id = %v, want %s", got, expectedProject)
				}
			}
			active, err := os.ReadFile(filepath.Join(dir, ".active"))
			if err != nil {
				t.Fatal(err)
			}
			if string(active) != "qa-base\n" {
				t.Errorf("transient flag changed persisted active profile: %q", active)
			}
		})
	}
}
