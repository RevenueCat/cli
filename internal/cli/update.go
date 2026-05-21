package cli

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const githubReleasesURL = "https://api.github.com/repos/RevenueCat/revenuecat-cli/releases/latest"

var errUpdateAvailable = errors.New("update available")

var updateHTTPClient = &http.Client{Timeout: 30 * time.Second}

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
	HTMLURL string        `json:"html_url"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func newUpdateCmd() *cobra.Command {
	var checkOnly bool

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update rc to the latest version",
		Long: `Check for a newer version of rc and install it.

The binary is downloaded directly from GitHub Releases — no Homebrew or
Xcode Command Line Tools required. The running binary is replaced atomically.

Not supported on Windows; download the latest release manually from
https://github.com/RevenueCat/revenuecat-cli/releases`,
		Example: `  rc update            # check and install if newer
  rc update --check    # check only, exit 1 if update available`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if runtime.GOOS == "windows" {
				return fmt.Errorf("rc update is not supported on Windows — download the latest release from https://github.com/RevenueCat/revenuecat-cli/releases")
			}

			rt := RuntimeFrom(cmd.Context())
			currentVersion := cmd.Root().Version

			if currentVersion == "dev" {
				rt.Out.Info("Running development build — skipping update.")
				return nil
			}

			rt.Out.Info("Checking for updates…")
			release, err := fetchLatestRelease(cmd.Context())
			if err != nil {
				return fmt.Errorf("checking for updates: %w", err)
			}

			latestTag := release.TagName
			latestVersion := strings.TrimPrefix(latestTag, "v")

			if latestVersion == currentVersion {
				if rt.Globals.JSON {
					return rt.Out.Render(map[string]any{
						"current_version": currentVersion,
						"latest_version":  latestVersion,
						"up_to_date":      true,
					})
				}
				rt.Out.Success(fmt.Sprintf("Already up to date (%s).", currentVersion))
				return nil
			}

			rt.Out.Info(fmt.Sprintf("Update available: %s → %s", currentVersion, latestVersion))

			if checkOnly {
				if rt.Globals.JSON {
					// Render JSON first, then return the sentinel so exit code is 1.
					_ = rt.Out.Render(map[string]any{
						"current_version": currentVersion,
						"latest_version":  latestVersion,
						"up_to_date":      false,
					})
				}
				return fmt.Errorf("%w: %s (current: %s)", errUpdateAvailable, latestVersion, currentVersion)
			}

			assetName := assetFilename(latestVersion)
			downloadURL := ""
			for _, a := range release.Assets {
				if a.Name == assetName {
					downloadURL = a.BrowserDownloadURL
					break
				}
			}
			if downloadURL == "" {
				return fmt.Errorf("no release asset found for %s/%s (looked for %q) — download manually: %s",
					runtime.GOOS, runtime.GOARCH, assetName, release.HTMLURL)
			}

			rt.Out.Info(fmt.Sprintf("Downloading %s…", assetName))
			tmpPath, err := downloadAsset(cmd.Context(), downloadURL, assetName)
			if err != nil {
				return fmt.Errorf("downloading update: %w", err)
			}
			defer os.Remove(tmpPath)

			execPath, err := os.Executable()
			if err != nil {
				return fmt.Errorf("finding current binary: %w", err)
			}
			execPath, err = filepath.EvalSymlinks(execPath)
			if err != nil {
				return fmt.Errorf("resolving binary path: %w", err)
			}

			if err := installBinary(tmpPath, execPath); err != nil {
				return fmt.Errorf("installing update: %w", err)
			}

			if rt.Globals.JSON {
				return rt.Out.Render(map[string]any{
					"previous_version": currentVersion,
					"current_version":  latestVersion,
					"up_to_date":       true,
				})
			}
			rt.Out.Success(fmt.Sprintf("Updated to %s.", latestVersion))
			return nil
		},
	}

	cmd.Flags().BoolVar(&checkOnly, "check", false, "check for updates without installing; exit 1 if update is available")
	return cmd
}

func fetchLatestRelease(ctx context.Context) (*githubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubReleasesURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := updateHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %s", resp.Status)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}
	return &release, nil
}

func assetFilename(version string) string {
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	if goos == "windows" {
		return fmt.Sprintf("rc_%s_%s_%s.zip", version, goos, goarch)
	}
	return fmt.Sprintf("rc_%s_%s_%s.tar.gz", version, goos, goarch)
}

// downloadAsset fetches the asset and extracts the rc binary to a temp file.
func downloadAsset(ctx context.Context, url, assetName string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := updateHTTPClient.Do(req)
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

	if strings.HasSuffix(assetName, ".zip") {
		// Write zip to disk first since zip.NewReader needs io.ReaderAt.
		_, copyErr := io.Copy(tmp, resp.Body)
		tmp.Close()
		if copyErr != nil {
			os.Remove(tmpName)
			return "", copyErr
		}
		if err := extractFromZip(tmpName); err != nil {
			os.Remove(tmpName)
			return "", err
		}
		return tmpName, nil
	}

	// tar.gz — extract directly into tmp
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
		if filepath.Base(hdr.Name) == "rc" && hdr.Typeflag == tar.TypeReg {
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

// extractFromZip reads a zip already written to path and overwrites it with
// the extracted rc binary.
func extractFromZip(path string) error {
	r, err := zip.OpenReader(path)
	if err != nil {
		return err
	}

	var found bool
	var tmpName string
	for _, f := range r.File {
		if filepath.Base(f.Name) == "rc.exe" || filepath.Base(f.Name) == "rc" {
			rc, err := f.Open()
			if err != nil {
				r.Close()
				return err
			}
			tmp, err := os.CreateTemp(filepath.Dir(path), "rc-zip-*")
			if err != nil {
				rc.Close()
				r.Close()
				return err
			}
			_, copyErr := io.Copy(tmp, rc)
			rc.Close()
			tmp.Close()
			if copyErr != nil {
				os.Remove(tmp.Name())
				r.Close()
				return copyErr
			}
			tmpName = tmp.Name()
			found = true
			break
		}
	}
	// Close the zip reader before renaming so we don't hold the file open.
	r.Close()

	if !found {
		return fmt.Errorf("rc binary not found in zip archive")
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// installBinary atomically replaces the current binary with the extracted one.
func installBinary(srcPath, destPath string) error {
	if err := os.Chmod(srcPath, 0755); err != nil {
		return err
	}

	// Write to a sibling temp file so Rename is atomic on the same filesystem.
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
