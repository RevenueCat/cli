package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempImage(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func runMediaAssetsUpload(t *testing.T, path string) (stdout, stderr string, err error) {
	t.Helper()
	t.Setenv("RC_CONFIG_DIR", t.TempDir())
	var out, errb bytes.Buffer
	root := NewRootCmd("test")
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetArgs([]string{"media-assets", "upload", path, "--no-input", "--api-key", "sk_test", "--project-id", "proj"})
	err = root.ExecuteContext(context.Background())
	return out.String(), errb.String(), err
}

func TestMediaAssetsUpload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/projects/proj/media_assets" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"object":"media_asset","id":"medas_abc","object_name":"media/proj/tiny.png","original_name":"tiny.png","original_size":1,"original_width":null,"original_height":null,"formats":null,"alt_text":null,"is_decorative":false,"asset_base_url":"https://assets.example.com","asset_type":"image","video_metadata":null,"transcoding_status":null}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("RC_BASE_URL", srv.URL)
	path := writeTempImage(t, "tiny.png", []byte{0x89, 'P', 'N', 'G'})

	stdout, stderr, err := runMediaAssetsUpload(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr, "Uploaded medas_abc (tiny.png, 1 KB)") {
		t.Fatalf("stderr missing success line: %s", stderr)
	}
	if !strings.Contains(stderr, "https://assets.example.com/media/proj/tiny.png") {
		t.Fatalf("stderr missing asset URL hint: %s", stderr)
	}
	if !strings.Contains(stdout, "medas_abc") {
		t.Fatalf("stdout missing rendered asset: %s", stdout)
	}
}

func runMediaAssetsList(t *testing.T, extra ...string) (stdout, stderr string, err error) {
	t.Helper()
	t.Setenv("RC_CONFIG_DIR", t.TempDir())
	var out, errb bytes.Buffer
	root := NewRootCmd("test")
	root.SetOut(&out)
	root.SetErr(&errb)
	args := []string{"media-assets", "list", "--no-input", "--api-key", "sk_test", "--project-id", "proj"}
	root.SetArgs(append(args, extra...))
	err = root.ExecuteContext(context.Background())
	return out.String(), errb.String(), err
}

func TestMediaAssetsList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/projects/proj/media_assets" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","items":[{"id":"medas_abc","object_name":"media/proj/hero.png","original_name":"hero.png","original_width":1024,"original_height":768,"asset_base_url":"https://assets.example.com","asset_type":"image"}],"next_page":"/v2/projects/proj/media_assets?starting_after=medas_next","url":"/v2/projects/proj/media_assets"}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("RC_BASE_URL", srv.URL)

	stdout, stderr, err := runMediaAssetsList(t)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"medas_abc", "hero.png", "1024x768", "https://assets.example.com/media/proj/hero.png"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q: %s", want, stdout)
		}
	}
	// The cursor comes from next_page's starting_after, not the item ID.
	if !strings.Contains(stderr, "--cursor medas_next") {
		t.Fatalf("stderr missing pagination hint: %s", stderr)
	}

	stdout, stderr, err = runMediaAssetsList(t, "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, `"next_page"`) {
		t.Fatalf("json output missing envelope: %s", stdout)
	}
	if strings.Contains(stderr, "--cursor") {
		t.Fatalf("hint must be suppressed under --json: %s", stderr)
	}
}

func TestMediaAssetsUpload_ValidationErrors(t *testing.T) {
	cases := []struct {
		name, file string
		data       []byte
		wantErr    string
	}{
		{"unsupported extension", "anim.gif", []byte("GIF89a"), "unsupported image type"},
		{"oversized", "big.png", make([]byte, 2<<20+1), "2 MiB"},
		{"empty", "empty.png", nil, "is empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeTempImage(t, tc.file, tc.data)
			_, _, err := runMediaAssetsUpload(t, path)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}
