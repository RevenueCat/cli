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

// DefaultReleasesURL is the GitHub API endpoint for the latest release.
const DefaultReleasesURL = "https://api.github.com/repos/RevenueCat/revenuecat-cli/releases/latest"

// maxBinaryBytes is a sanity cap on the extracted binary size (64 MiB).
const maxBinaryBytes = 64 << 20

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

// FetchRelease returns the latest release metadata from the given URL.
// Pass DefaultReleasesURL in production; tests may pass an httptest.Server URL.
func FetchRelease(ctx context.Context, hc *http.Client, url string) (*Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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

// DownloadBinary fetches the release asset matching goos/goarch, extracts the
// rc binary, and returns the path to a temporary file. The caller must remove
// it when done. Returns an error for Windows (zip format not supported).
func DownloadBinary(ctx context.Context, hc *http.Client, r *Release, goos, goarch string) (string, error) {
	if goos == "windows" {
		return "", fmt.Errorf("DownloadBinary does not support Windows — download manually: %s", r.HTMLURL)
	}
	assetName := archiveName(r.Version(), goos, goarch)
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
	return downloadTarGz(ctx, hc, url)
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
	tmpName := tmp.Name()

	// Clean up the temp file on any failure path.
	installed := false
	defer func() {
		if !installed {
			os.Remove(tmpName)
		}
	}()

	// Write directly into the temp file we just created — no TOCTOU gap.
	src, err := os.Open(srcPath)
	if err != nil {
		tmp.Close()
		return err
	}
	_, copyErr := io.Copy(tmp, src)
	src.Close()
	if err := tmp.Close(); err != nil && copyErr == nil {
		copyErr = err
	}
	if copyErr != nil {
		return copyErr
	}

	if err := os.Chmod(tmpName, 0755); err != nil {
		return err
	}
	if err := os.Rename(tmpName, destPath); err != nil {
		return err
	}
	installed = true
	return nil
}

// IsNewer reports whether latest is strictly greater than current.
//
// Pre-release handling: if the numeric parts are equal and current has a
// pre-release suffix while latest does not, latest is considered newer (a
// stable release supersedes its own pre-release). If latest has a pre-release
// suffix and current does not, we never downgrade.
func IsNewer(latest, current string) bool {
	latestNums, latestPre := parseSemver(latest)
	currentNums, currentPre := parseSemver(current)
	if latestNums == nil || currentNums == nil {
		return false
	}
	for i := range 3 {
		if latestNums[i] != currentNums[i] {
			return latestNums[i] > currentNums[i]
		}
	}
	// Numeric parts are equal — a stable release is newer than a pre-release.
	return currentPre && !latestPre
}

// parseSemver parses "X.Y.Z" or "vX.Y.Z[-pre]" and returns ([major,minor,patch], hasPre).
func parseSemver(v string) ([]int, bool) {
	v = strings.TrimPrefix(v, "v")
	// Split off pre-release suffix before splitting on dots.
	core, pre, _ := strings.Cut(v, "-")
	hasPre := pre != ""
	parts := strings.SplitN(core, ".", 3)
	if len(parts) != 3 {
		return nil, false
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n := 0
		for _, c := range p {
			if c < '0' || c > '9' {
				return nil, false
			}
			n = n*10 + int(c-'0')
		}
		nums[i] = n
	}
	return nums, hasPre
}

func archiveName(version, goos, goarch string) string {
	if goos == "windows" {
		return fmt.Sprintf("rc_%s_%s_%s.zip", version, goos, goarch)
	}
	return fmt.Sprintf("rc_%s_%s_%s.tar.gz", version, goos, goarch)
}

// downloadTarGz downloads the tar.gz at url and extracts only the rc binary.
func downloadTarGz(ctx context.Context, hc *http.Client, url string) (string, error) {
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
		// Require the entry to be named exactly "rc" with no path components.
		// filepath.Clean is intentionally avoided here — it can normalise
		// traversal sequences like "bin/../rc" into "rc" and bypass the check.
		if hdr.Name == "rc" && hdr.Typeflag == tar.TypeReg {
			lr := io.LimitReader(tr, maxBinaryBytes+1)
			n, err := io.Copy(tmp, lr)
			if err != nil {
				tmp.Close()
				os.Remove(tmpName)
				return "", err
			}
			if n > maxBinaryBytes {
				tmp.Close()
				os.Remove(tmpName)
				return "", fmt.Errorf("binary exceeds maximum allowed size (%d MiB)", maxBinaryBytes>>20)
			}
			if err := tmp.Close(); err != nil {
				os.Remove(tmpName)
				return "", fmt.Errorf("flushing extracted binary: %w", err)
			}
			return tmpName, nil
		}
	}

	tmp.Close()
	os.Remove(tmpName)
	return "", fmt.Errorf("rc binary not found in archive")
}
