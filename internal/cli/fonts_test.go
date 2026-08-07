package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func runFontsUpload(t *testing.T, path string) (stdout, stderr string, err error) {
	t.Helper()
	t.Setenv("RC_CONFIG_DIR", t.TempDir())
	var out, errb bytes.Buffer
	root := NewRootCmd("test")
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetArgs([]string{"fonts", "upload", path, "--no-input", "--api-key", "sk_test", "--project-id", "proj"})
	err = root.ExecuteContext(context.Background())
	return out.String(), errb.String(), err
}

func TestFontsUpload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/projects/proj/fonts" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"object":"font","id":"font_abc","name":"Inter Bold","family_name":"Inter","style":"bold","weight":700,"url":"https://assets.example.com/fonts/inter-bold.ttf","font_key":"RCFM:abc123"}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("RC_BASE_URL", srv.URL)
	path := writeTempImage(t, "inter-bold.ttf", []byte{0x00, 0x01, 0x00, 0x00})

	stdout, stderr, err := runFontsUpload(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr, "Uploaded font_abc (Inter Bold, bold 700)") {
		t.Fatalf("stderr missing success line: %s", stderr)
	}
	if !strings.Contains(stderr, "RCFM:abc123") {
		t.Fatalf("stderr missing font_key hint: %s", stderr)
	}
	if !strings.Contains(stdout, "font_abc") {
		t.Fatalf("stdout missing rendered font: %s", stdout)
	}
}

func TestFontsUpload_Conflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"type":"resource_already_exists","message":"A font with this style already exists for this project"}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("RC_BASE_URL", srv.URL)
	path := writeTempImage(t, "inter-bold.ttf", []byte{0x00, 0x01, 0x00, 0x00})

	_, stderr, err := runFontsUpload(t, path)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(stderr, "rc fonts list") {
		t.Fatalf("stderr missing fonts list hint: %s", stderr)
	}
}

func runFontsList(t *testing.T, extra ...string) (stdout, stderr string, err error) {
	t.Helper()
	t.Setenv("RC_CONFIG_DIR", t.TempDir())
	var out, errb bytes.Buffer
	root := NewRootCmd("test")
	root.SetOut(&out)
	root.SetErr(&errb)
	args := []string{"fonts", "list", "--no-input", "--api-key", "sk_test", "--project-id", "proj"}
	root.SetArgs(append(args, extra...))
	err = root.ExecuteContext(context.Background())
	return out.String(), errb.String(), err
}

func TestFontsList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/projects/proj/fonts" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","items":[{"object":"font","id":"font_abc","name":"Inter Bold","family_name":"Inter","style":"bold","weight":700,"url":"https://assets.example.com/fonts/inter-bold.ttf","font_key":"RCFM:abc123"}],"next_page":"/v2/projects/proj/fonts?starting_after=font_abc","url":"/v2/projects/proj/fonts"}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("RC_BASE_URL", srv.URL)

	stdout, stderr, err := runFontsList(t)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"font_abc", "Inter Bold", "Inter", "bold", "700", "RCFM:abc123"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q: %s", want, stdout)
		}
	}
	if !strings.Contains(stderr, "--cursor font_abc") {
		t.Fatalf("stderr missing pagination hint: %s", stderr)
	}

	stdout, _, err = runFontsList(t, "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, `"next_page"`) {
		t.Fatalf("json output missing envelope: %s", stdout)
	}
}

func TestFontsUpload_ValidationErrors(t *testing.T) {
	cases := []struct {
		name, file string
		data       []byte
		wantErr    string
	}{
		{"unsupported extension", "web.woff", []byte("wOFF"), "unsupported font type"},
		{"oversized", "big.ttf", make([]byte, 5<<20+1), "5 MiB"},
		{"empty", "empty.ttf", nil, "is empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeTempImage(t, tc.file, tc.data)
			_, _, err := runFontsUpload(t, path)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}
