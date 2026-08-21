package google

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/oauth2"
	"google.golang.org/api/option"
	playreporting "google.golang.org/api/playdeveloperreporting/v1beta1"
)

// PlayApp is an app accessible to the signed-in user in Play.
type PlayApp struct {
	PackageName string
	DisplayName string
}

// ListPlayApps returns the Play apps the signed-in user can access, via the
// Play Developer Reporting API's apps.search — the only endpoint that lists
// apps without already knowing the package name or developer ID. Requires the
// ScopePlayReporting scope and the Play Developer Reporting API enabled.
func ListPlayApps(ctx context.Context, ts oauth2.TokenSource) ([]PlayApp, error) {
	svc, err := playreporting.NewService(ctx, option.WithTokenSource(ts))
	if err != nil {
		return nil, fmt.Errorf("play developer reporting client: %w", err)
	}
	var apps []PlayApp
	err = svc.Apps.Search().PageSize(1000).Pages(ctx, func(page *playreporting.GooglePlayDeveloperReportingV1beta1SearchAccessibleAppsResponse) error {
		for _, a := range page.Apps {
			apps = append(apps, PlayApp{PackageName: a.PackageName, DisplayName: a.DisplayName})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list Play apps: %w", err)
	}
	return apps, nil
}

var (
	applicationIDPattern = regexp.MustCompile(`applicationId\s*[=(]?\s*["']([a-zA-Z0-9_.]+)["']`)
	manifestPackage      = regexp.MustCompile(`package\s*=\s*["']([a-zA-Z0-9_.]+)["']`)
)

// DetectPackageName tries to read the Android application/package id from the
// project rooted at dir, so the developer doesn't have to type it. Checks the
// common Gradle and manifest locations for native Android, Flutter, and React
// Native layouts. Returns "" if nothing reliable is found.
func DetectPackageName(dir string) string {
	gradleFiles := []string{
		"android/app/build.gradle.kts",
		"android/app/build.gradle",
		"app/build.gradle.kts",
		"app/build.gradle",
		"build.gradle.kts",
		"build.gradle",
	}
	for _, rel := range gradleFiles {
		if id := firstMatch(filepath.Join(dir, rel), applicationIDPattern); id != "" {
			return id
		}
	}
	manifests := []string{
		"android/app/src/main/AndroidManifest.xml",
		"app/src/main/AndroidManifest.xml",
		"src/main/AndroidManifest.xml",
		"AndroidManifest.xml",
	}
	for _, rel := range manifests {
		if id := firstMatch(filepath.Join(dir, rel), manifestPackage); id != "" {
			return id
		}
	}
	return ""
}

func firstMatch(path string, re *regexp.Regexp) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if m := re.FindSubmatch(data); m != nil {
		return strings.TrimSpace(string(m[1]))
	}
	return ""
}
