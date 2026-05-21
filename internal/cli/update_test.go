package cli_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/revenuecat/cli/internal/cli"
)

// serveRelease starts a test server returning the given release JSON and sets
// RC_UPDATER_RELEASES_URL so the update command picks it up without any
// package-level state mutation.
func serveRelease(t *testing.T, tagName string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"tag_name": tagName,
			"html_url": "https://github.com/example",
			"assets": []map[string]any{{
				"name":                 "rc_" + tagName[1:] + "_darwin_arm64.tar.gz",
				"browser_download_url": "http://" + r.Host + "/download",
			}},
		})
	}))
	t.Setenv("RC_UPDATER_RELEASES_URL", srv.URL)
	t.Cleanup(srv.Close)
	return srv
}

// TestUpdate_DevBuild_JSON verifies that a dev build emits JSON with
// development_build=true and exits 0 — not silence.
func TestUpdate_DevBuild_JSON(t *testing.T) {
	t.Setenv("RC_CONFIG_DIR", t.TempDir())
	var out, errBuf bytes.Buffer
	root := newRootWithBuffers(t, "dev", &out, &errBuf)
	root.SetArgs([]string{"update", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if errBuf.Len() != 0 {
		t.Errorf("--json must not write to stderr; got %q", errBuf.String())
	}
	var got struct {
		Data struct {
			DevBuild  bool   `json:"development_build"`
			UpToDate  bool   `json:"up_to_date"`
			Installed string `json:"installed_version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if !got.Data.DevBuild {
		t.Error("want development_build=true")
	}
	if !got.Data.UpToDate {
		t.Error("want up_to_date=true for dev build")
	}
}

// TestUpdate_UpToDate_JSON verifies the up-to-date JSON shape.
// runCmd uses version "test" (non-semver) so IsNewer returns false.
func TestUpdate_UpToDate_JSON(t *testing.T) {
	serveRelease(t, "v1.2.3")

	out, errb, err := runCmd(t, "update", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, errb)
	}
	if errb != "" {
		t.Errorf("--json must not write to stderr; got %q", errb)
	}
	var got struct {
		Data struct {
			UpToDate  bool   `json:"up_to_date"`
			Installed string `json:"installed_version"`
			Latest    string `json:"latest_version"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if !got.Data.UpToDate {
		t.Errorf("want up_to_date=true, got false")
	}
	if got.Data.Latest != "1.2.3" {
		t.Errorf("want latest_version=1.2.3, got %q", got.Data.Latest)
	}
}

// TestUpdate_Check_UpdateAvailable_JSON verifies --check --json writes exactly
// one JSON document to stdout and exits 1 with nothing on stderr.
func TestUpdate_Check_UpdateAvailable_JSON(t *testing.T) {
	serveRelease(t, "v9.9.9")

	var out, errBuf bytes.Buffer
	root := newRootWithBuffers(t, "1.0.0", &out, &errBuf)
	root.SetArgs([]string{"update", "--check", "--json"})
	execErr := root.Execute()

	if execErr == nil {
		t.Fatal("want non-zero exit for --check when update is available")
	}
	if errBuf.Len() != 0 {
		t.Errorf("--check --json must not write to stderr; got %q", errBuf.String())
	}
	var got struct {
		Data struct {
			UpToDate  bool   `json:"up_to_date"`
			Installed string `json:"installed_version"`
			Latest    string `json:"latest_version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, out.String())
	}
	if got.Data.UpToDate {
		t.Error("want up_to_date=false")
	}
	if got.Data.Latest != "9.9.9" {
		t.Errorf("want latest_version=9.9.9, got %q", got.Data.Latest)
	}
}

// TestUpdate_Check_UpdateAvailable_Human verifies human-mode --check exits 1
// with nothing on stdout.
func TestUpdate_Check_UpdateAvailable_Human(t *testing.T) {
	serveRelease(t, "v9.9.9")

	var out, errBuf bytes.Buffer
	root := newRootWithBuffers(t, "1.0.0", &out, &errBuf)
	root.SetArgs([]string{"update", "--check"})
	if err := root.Execute(); err == nil {
		t.Fatal("want non-zero exit")
	}
	if out.Len() != 0 {
		t.Errorf("human mode should not write to stdout; got %q", out.String())
	}
}

// TestUpdate_ConsistentJSONSchema verifies the four stable keys are present
// on every JSON output path.
func TestUpdate_ConsistentJSONSchema(t *testing.T) {
	serveRelease(t, "v1.0.0")

	out, errb, err := runCmd(t, "update", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v\nstderr: %s", err, errb)
	}
	var got struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	for _, key := range []string{"installed_version", "latest_version", "up_to_date", "updated"} {
		if _, ok := got.Data[key]; !ok {
			t.Errorf("JSON output missing key %q", key)
		}
	}
}

// newRootWithBuffers builds a root command wired to explicit output buffers.
func newRootWithBuffers(t *testing.T, version string, out, errBuf *bytes.Buffer) interface {
	SetArgs([]string)
	Execute() error
} {
	t.Helper()
	t.Setenv("RC_CONFIG_DIR", t.TempDir())
	t.Setenv("RC_API_KEY", "")
	t.Setenv("RC_PROJECT_ID", "")
	t.Setenv("RC_BASE_URL", "")
	t.Setenv("RC_PROFILE", "")
	root := cli.NewRootCmd(version)
	root.SetOut(out)
	root.SetErr(errBuf)
	return root
}
