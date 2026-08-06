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

func mediaAssetServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/projects/proj/media_assets" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{"object":"media_asset","id":"medas_abc","object_name":"media/proj/tiny.png","original_name":"tiny.png","original_size":1,"original_width":null,"original_height":null,"formats":null,"alt_text":null,"is_decorative":false,"asset_base_url":"https://assets.example.com","asset_type":"image","video_metadata":null,"transcoding_status":null}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func writeTempImage(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func runMediaAssetsUpload(t *testing.T, path string, extra ...string) (stdout, stderr string, err error) {
	t.Helper()
	t.Setenv("RC_CONFIG_DIR", t.TempDir())
	var out, errb bytes.Buffer
	root := NewRootCmd("test")
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetArgs(append([]string{"media-assets", "upload", path, "--no-input", "--api-key", "sk_test", "--project-id", "proj"}, extra...))
	err = root.ExecuteContext(context.Background())
	return out.String(), errb.String(), err
}

func TestMediaAssetsUpload_JSON(t *testing.T) {
	srv := mediaAssetServer(t)
	t.Setenv("RC_BASE_URL", srv.URL)
	path := writeTempImage(t, "tiny.png", []byte{0x89, 'P', 'N', 'G'})

	stdout, stderr, err := runMediaAssetsUpload(t, path, "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, `"id": "medas_abc"`) || !strings.Contains(stdout, `"schema_version": 1`) {
		t.Fatalf("stdout missing JSON envelope: %s", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr should be empty in JSON mode: %s", stderr)
	}
}

func TestMediaAssetsUpload_Human(t *testing.T) {
	srv := mediaAssetServer(t)
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

func TestMediaAssetsUpload_ValidationErrors(t *testing.T) {
	// No server: validation must fail before any network call.
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
