package cli_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/revenuecat/cli/internal/cli"
	"github.com/revenuecat/cli/internal/updater"
)

// serveRelease starts a test server that returns the given release JSON and
// optionally serves the archive at /download. It sets updater.ReleasesURL and
// restores the original value via t.Cleanup.
func serveRelease(t *testing.T, tagName string, archiveData []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/download" {
			w.Write(archiveData)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"tag_name": tagName,
			"html_url": "https://github.com/example",
			"assets": []map[string]any{{
				"name":                 "rc_" + tagName[1:] + "_darwin_arm64.tar.gz",
				"browser_download_url": "http://" + r.Host + "/download",
			}},
		})
	}))
	orig := updater.ReleasesURL
	updater.ReleasesURL = srv.URL
	t.Cleanup(func() {
		srv.Close()
		updater.ReleasesURL = orig
	})
	return srv
}

// TestUpdate_DevBuild_JSON verifies that a dev build emits JSON with
// development_build=true and exits 0 — not silence.
func TestUpdate_DevBuild_JSON(t *testing.T) {
	// Use version "dev" (the default for NewRootCmd("dev")).
	t.Setenv("RC_CONFIG_DIR", t.TempDir())
	var out, errBuf bytes.Buffer
	root := newRootWithBuffers(t, "dev", &out, &errBuf)
	root.SetArgs([]string{"update", "--json"})
	err := root.Execute()
	if err != nil {
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
func TestUpdate_UpToDate_JSON(t *testing.T) {
	serveRelease(t, "v1.2.3", nil)
	t.Setenv("RC_CONFIG_DIR", t.TempDir())

	out, errb, err := runCmd(t, "update", "--json")
	// runCmd uses version "test" which is not a valid semver, so IsNewer returns
	// false and we get the up-to-date path.
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

// TestUpdate_Check_UpdateAvailable_JSON verifies that --check --json writes one
// JSON document to stdout and exits 1, with nothing on stderr (no second
// error envelope).
func TestUpdate_Check_UpdateAvailable_JSON(t *testing.T) {
	serveRelease(t, "v9.9.9", nil)
	t.Setenv("RC_CONFIG_DIR", t.TempDir())

	// runCmd uses version "test" (non-semver) so IsNewer("9.9.9","test")=false
	// and we get the up-to-date path. We need a real semver current version.
	// Use a fresh root with version "1.0.0".
	var out, errBuf bytes.Buffer
	root := newRootWithBuffers(t, "1.0.0", &out, &errBuf)
	root.SetArgs([]string{"update", "--check", "--json"})
	execErr := root.Execute()

	// Should exit non-zero.
	if execErr == nil {
		t.Fatal("want non-zero exit for --check when update is available")
	}
	// stderr must be empty — no second JSON error envelope.
	if errBuf.Len() != 0 {
		t.Errorf("--check --json must not write to stderr; got %q", errBuf.String())
	}
	// stdout must be valid JSON with up_to_date=false.
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

// TestUpdate_Check_UpdateAvailable_Human verifies the human-mode --check path
// exits non-zero with an informative message.
func TestUpdate_Check_UpdateAvailable_Human(t *testing.T) {
	serveRelease(t, "v9.9.9", nil)
	t.Setenv("RC_CONFIG_DIR", t.TempDir())

	var out, errBuf bytes.Buffer
	root := newRootWithBuffers(t, "1.0.0", &out, &errBuf)
	root.SetArgs([]string{"update", "--check"})
	err := root.Execute()
	if err == nil {
		t.Fatal("want non-zero exit")
	}
	if out.Len() != 0 {
		t.Errorf("human mode should not write to stdout; got %q", out.String())
	}
}

// TestUpdate_ConsistentJSONSchema verifies that installed_version/latest_version/
// up_to_date/updated are present in all JSON output paths.
func TestUpdate_ConsistentJSONSchema(t *testing.T) {
	serveRelease(t, "v1.0.0", nil)
	t.Setenv("RC_CONFIG_DIR", t.TempDir())

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

// newRootWithBuffers builds a root command wired to explicit buffers so tests
// can capture output independently of runCmd's version string.
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
