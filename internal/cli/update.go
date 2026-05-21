package cli

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/tui"
	"github.com/revenuecat/cli/internal/updater"
)

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

			hc := &http.Client{Timeout: 30 * time.Second}

			rt.Out.Info("Checking for updates…")
			release, err := updater.FetchRelease(cmd.Context(), hc)
			if err != nil {
				return fmt.Errorf("checking for updates: %w", err)
			}

			latestVersion := release.Version()

			if !updater.IsNewer(latestVersion, currentVersion) {
				if rt.Globals.JSON {
					return rt.Out.Render(map[string]any{
						"installed_version": currentVersion,
						"latest_version":    latestVersion,
						"up_to_date":        true,
						"updated":           false,
					})
				}
				rt.Out.Success(fmt.Sprintf("Already up to date (%s).", currentVersion))
				return nil
			}

			rt.Out.Info(fmt.Sprintf("Update available: %s → %s", currentVersion, latestVersion))

			if checkOnly {
				if rt.Globals.JSON {
					// Write the JSON result to stdout, then return SilentExitError so
					// run.go exits 1 without also emitting a JSON error envelope.
					if err := rt.Out.Render(map[string]any{
						"installed_version": currentVersion,
						"latest_version":    latestVersion,
						"up_to_date":        false,
						"updated":           false,
					}); err != nil {
						return err
					}
					return &SilentExitError{Code: 1}
				}
				return fmt.Errorf("update available: %s (current: %s)", latestVersion, currentVersion)
			}

			if !rt.Globals.AssumeYes {
				ok, err := tui.Confirm(rt.Globals.NoInput,
					fmt.Sprintf("Update rc from %s to %s?", currentVersion, latestVersion))
				if err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("aborted")
				}
			}

			rt.Out.Info(fmt.Sprintf("Downloading %s…", release.AssetName(runtime.GOOS, runtime.GOARCH)))
			tmpPath, err := updater.DownloadBinary(cmd.Context(), hc, release, runtime.GOOS, runtime.GOARCH)
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

			if err := updater.Install(tmpPath, execPath); err != nil {
				return fmt.Errorf("installing update: %w", err)
			}

			if rt.Globals.JSON {
				return rt.Out.Render(map[string]any{
					"installed_version": latestVersion,
					"latest_version":    latestVersion,
					"up_to_date":        true,
					"updated":           true,
				})
			}
			rt.Out.Success(fmt.Sprintf("Updated to %s.", latestVersion))
			return nil
		},
	}

	cmd.Flags().BoolVar(&checkOnly, "check", false, "check for updates without installing; exit 1 if update is available")
	return cmd
}
