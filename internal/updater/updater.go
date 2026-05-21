// Package updater fetches and installs rc releases from GitHub.
package updater

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const releasesURL = "https://api.github.com/repos/RevenueCat/revenuecat-cli/releases/latest"

type Release struct {
	TagName string  `json:"tag_name"`
	HTMLURL string  `json:"html_url"`
	Assets  []Asset `json:"assets"`
}

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Version returns the release version without the leading "v".
func (r *Release) Version() string {
	return strings.TrimPrefix(r.TagName, "v")
}

// AssetName returns the expected archive filename for the given platform.
func (r *Release) AssetName(goos, goarch string) string {
	return archiveName(r.Version(), goos, goarch)
}

// LatestVersion returns the version string (without leading "v") of the latest
// published release.
func LatestVersion(ctx context.Context, hc *http.Client) (string, error) {
	r, err := fetchRelease(ctx, hc)
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(r.TagName, "v"), nil
}

// DownloadBinary fetches the release asset matching goos/goarch, extracts the
// rc binary, and returns the path to a temporary file. The caller must remove
// it when done.
func DownloadBinary(ctx context.Context, hc *http.Client, r *Release, goos, goarch string) (string, error) {
	assetName := archiveName(strings.TrimPrefix(r.TagName, "v"), goos, goarch)
	url := ""
	for _, a := range r.Assets {
		if a.Name == assetName {
			url = a.BrowserDownloadURL
			break
		}
	}
	if url == "" {
		return "", fmt.Errorf("no release asset for %s/%s (looked for %q) — download manually: %s",
			goos, goarch, assetName, r.HTMLURL)
	}
	return downloadBinary(ctx, hc, url)
}

// FetchRelease returns the latest release metadata.
func FetchRelease(ctx context.Context, hc *http.Client) (*Release, error) {
	return fetchRelease(ctx, hc)
}

// Install atomically replaces destPath with the binary at srcPath.
func Install(srcPath, destPath string) error {
	if err := os.Chmod(srcPath, 0755); err != nil {
		return err
	}
	dir := filepath.Dir(destPath)
	tmp, err := os.CreateTemp(dir, ".rc-update-*")
	if err != nil {
		return fmt.Errorf("cannot write to %s (check permissions): %w", dir, err)
	}
	tmp.Close()
	os.Remove(tmp.Name())

	if err := copyFile(srcPath, tmp.Name()); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0755); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), destPath)
}

// IsNewer reports whether latest is strictly greater than current using semver.
// Returns false when versions are equal, unparseable, or latest looks like a
// pre-release/snapshot relative to current.
func IsNewer(latest, current string) bool {
	lp := parseSemver(latest)
	cp := parseSemver(current)
	if lp == nil || cp == nil {
		return false
	}
	if lp[0] != cp[0] {
		return lp[0] > cp[0]
	}
	if lp[1] != cp[1] {
		return lp[1] > cp[1]
	}
	return lp[2] > cp[2]
}

func parseSemver(v string) []int {
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 3)
	if len(parts) != 3 {
		return nil
	}
	nums := make([]int, 3)
	for i, p := range parts {
		p, _, _ = strings.Cut(p, "-")
		n := 0
		for _, c := range p {
			if c < '0' || c > '9' {
				return nil
			}
			n = n*10 + int(c-'0')
		}
		nums[i] = n
	}
	return nums
}

func fetchRelease(ctx context.Context, hc *http.Client) (*Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "revenuecat-cli")

	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("GitHub API returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var r Release
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	return &r, nil
}

func archiveName(version, goos, goarch string) string {
	if goos == "windows" {
		return fmt.Sprintf("rc_%s_%s_%s.zip", version, goos, goarch)
	}
	return fmt.Sprintf("rc_%s_%s_%s.tar.gz", version, goos, goarch)
}

// downloadBinary downloads the archive at url and extracts only the rc binary
// to a temp file.
func downloadBinary(ctx context.Context, hc *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned %s", resp.Status)
	}

	tmp, err := os.CreateTemp("", "rc-update-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return "", err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			tmp.Close()
			os.Remove(tmpName)
			return "", err
		}
		// Accept only the top-level binary; reject path traversal attempts.
		if filepath.Base(hdr.Name) == "rc" && hdr.Typeflag == tar.TypeReg &&
			!strings.Contains(hdr.Name, "..") {
			if _, err := io.Copy(tmp, tr); err != nil {
				tmp.Close()
				os.Remove(tmpName)
				return "", err
			}
			tmp.Close()
			return tmpName, nil
		}
	}

	tmp.Close()
	os.Remove(tmpName)
	return "", fmt.Errorf("rc binary not found in archive")
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
