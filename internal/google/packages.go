package google

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

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
