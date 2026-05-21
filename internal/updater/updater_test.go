package updater_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/revenuecat/cli/internal/updater"
)

// --- IsNewer ---

func TestIsNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		// Strictly newer
		{"1.2.3", "1.2.2", true},
		{"1.3.0", "1.2.9", true},
		{"2.0.0", "1.9.9", true},
		// Equal
		{"1.2.3", "1.2.3", false},
		// Older
		{"1.2.2", "1.2.3", false},
		{"1.0.0", "2.0.0", false},
		// v-prefix stripped
		{"v1.2.3", "1.2.2", true},
		{"1.2.3", "v1.2.2", true},
		// Pre-release: stable supersedes its pre-release
		{"1.0.0", "1.0.0-rc1", true},
		{"1.0.0", "1.0.0-beta.1", true},
		// Pre-release latest never downgrades stable
		{"1.0.0-rc1", "1.0.0", false},
		// Both pre-release same numbers → not newer
		{"1.0.0-rc2", "1.0.0-rc1", false},
		// Unparseable → not newer
		{"not-a-version", "1.0.0", false},
		{"1.0.0", "not-a-version", false},
		{"", "1.0.0", false},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s_vs_%s", tc.latest, tc.current), func(t *testing.T) {
			got := updater.IsNewer(tc.latest, tc.current)
			if got != tc.want {
				t.Errorf("IsNewer(%q, %q) = %v, want %v", tc.latest, tc.current, got, tc.want)
			}
		})
	}
}

// --- FetchRelease ---

func TestFetchRelease_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("expected User-Agent header")
		}
		json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "v1.2.3",
			"html_url": "https://github.com/example",
			"assets": []map[string]any{
				{"name": "rc_1.2.3_darwin_arm64.tar.gz", "browser_download_url": "https://example.com/rc.tar.gz"},
			},
		})
	}))
	defer srv.Close()

	r, err := updater.FetchRelease(context.Background(), &http.Client{}, srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Version() != "1.2.3" {
		t.Errorf("want version 1.2.3, got %q", r.Version())
	}
	if len(r.Assets) != 1 {
		t.Errorf("want 1 asset, got %d", len(r.Assets))
	}
}

func TestFetchRelease_NonOKIncludesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"rate limited"}`)
	}))
	defer srv.Close()

	_, err := updater.FetchRelease(context.Background(), &http.Client{}, srv.URL)
	if err == nil {
		t.Fatal("expected error")
	}
	if msg := err.Error(); !contains(msg, "403") || !contains(msg, "rate limited") {
		t.Errorf("error should include status and body, got: %v", err)
	}
}

// --- DownloadBinary ---

func TestDownloadBinary_Success(t *testing.T) {
	const fakeContent = "#!/bin/sh\necho hello"
	tarGzData := makeTarGzNamed(t, "rc", []byte(fakeContent))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(tarGzData)
	}))
	defer srv.Close()

	release := &updater.Release{
		TagName: "v1.2.3",
		HTMLURL: "https://example.com",
		Assets:  []updater.Asset{{Name: "rc_1.2.3_linux_amd64.tar.gz", BrowserDownloadURL: srv.URL + "/rc.tar.gz"}},
	}

	path, err := updater.DownloadBinary(context.Background(), &http.Client{}, release, "linux", "amd64")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer os.Remove(path)

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading extracted binary: %v", err)
	}
	if string(got) != fakeContent {
		t.Errorf("want %q, got %q", fakeContent, string(got))
	}
}

func TestDownloadBinary_WindowsUnsupported(t *testing.T) {
	r := &updater.Release{TagName: "v1.0.0", HTMLURL: "https://example.com"}
	_, err := updater.DownloadBinary(context.Background(), &http.Client{}, r, "windows", "amd64")
	if err == nil {
		t.Fatal("expected error for windows")
	}
}

func TestDownloadBinary_MissingAsset(t *testing.T) {
	r := &updater.Release{TagName: "v1.0.0", HTMLURL: "https://example.com", Assets: nil}
	_, err := updater.DownloadBinary(context.Background(), &http.Client{}, r, "linux", "amd64")
	if err == nil {
		t.Fatal("expected error for missing asset")
	}
}

func TestDownloadBinary_RejectsNestedPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(makeTarGzNamed(t, "bin/rc", []byte("fake")))
	}))
	defer srv.Close()

	release := &updater.Release{
		TagName: "v1.0.0",
		HTMLURL: "https://example.com",
		Assets:  []updater.Asset{{Name: "rc_1.0.0_linux_amd64.tar.gz", BrowserDownloadURL: srv.URL}},
	}
	_, err := updater.DownloadBinary(context.Background(), &http.Client{}, release, "linux", "amd64")
	if err == nil {
		t.Fatal("expected error: nested path bin/rc should be rejected")
	}
}

func TestDownloadBinary_RejectsTraversalBypass(t *testing.T) {
	// "bin/../rc" normalises to "rc" via filepath.Clean — must still be rejected.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(makeTarGzNamed(t, "bin/../rc", []byte("fake")))
	}))
	defer srv.Close()

	release := &updater.Release{
		TagName: "v1.0.0",
		HTMLURL: "https://example.com",
		Assets:  []updater.Asset{{Name: "rc_1.0.0_linux_amd64.tar.gz", BrowserDownloadURL: srv.URL}},
	}
	_, err := updater.DownloadBinary(context.Background(), &http.Client{}, release, "linux", "amd64")
	if err == nil {
		t.Fatal("expected error: traversal path bin/../rc should be rejected")
	}
}

// --- Install ---

func TestInstall_ReplacesDestination(t *testing.T) {
	src := writeTempFile(t, []byte("new binary content"))
	dest := writeTempFile(t, []byte("old binary content"))

	if err := updater.Install(src, dest); err != nil {
		t.Fatalf("Install: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("reading dest: %v", err)
	}
	if string(got) != "new binary content" {
		t.Errorf("dest not updated: got %q", got)
	}
}

func TestInstall_DestIsExecutable(t *testing.T) {
	src := writeTempFile(t, []byte("#!/bin/sh"))
	dest := writeTempFile(t, []byte("old"))

	if err := updater.Install(src, dest); err != nil {
		t.Fatalf("Install: %v", err)
	}

	info, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Errorf("dest should be executable, mode=%v", info.Mode())
	}
}

func TestInstall_CleansUpTempOnFailure(t *testing.T) {
	src := writeTempFile(t, []byte("content"))
	dir := t.TempDir()
	// Destination in a read-only directory — Install should fail.
	if err := os.Chmod(dir, 0555); err != nil {
		t.Skip("cannot make dir read-only:", err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0755) })

	dest := dir + "/rc"
	err := updater.Install(src, dest)
	if err == nil {
		t.Fatal("expected error installing to read-only dir")
	}

	// No temp files should be left behind in the dir.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("temp file left behind after failed install: %v", entries)
	}
}

// --- helpers ---

func writeTempFile(t *testing.T, content []byte) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "updater-test-*")
	if err != nil {
		t.Fatal(err)
	}
	f.Write(content)
	f.Close()
	return f.Name()
}

func makeTarGzNamed(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	_ = tw.WriteHeader(&tar.Header{
		Name:     name,
		Typeflag: tar.TypeReg,
		Size:     int64(len(content)),
		Mode:     0755,
	})
	tw.Write(content)
	tw.Close()
	gw.Close()
	return buf.Bytes()
}

func contains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
