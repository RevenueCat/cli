package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectAppProject(t *testing.T) {
	cases := []struct {
		name         string
		files        []string
		wantLabel    string
		wantPlatform string
		wantStatus   projectStatus
	}{
		{"xcode", []string{"MyApp.xcodeproj/project.pbxproj"}, "Xcode project (MyApp.xcodeproj)", "ios", projectClear},
		{"flutter", []string{"pubspec.yaml"}, "Flutter app", "cross", projectClear},
		{"react-native", []string{"package.json:{\"dependencies\":{\"react-native\":\"0.74\"}}"}, "React Native app", "cross", projectClear},
		{"android", []string{"settings.gradle"}, "Android project", "android", projectClear},
		{"tuist", []string{"Project.swift"}, "Tuist project (iOS)", "ios", projectClear},
		// A CocoaPods iOS project surfaces twice but is still one clear app.
		{"xcode-with-workspace", []string{"MyApp.xcodeproj/project.pbxproj", "MyApp.xcworkspace/contents.xcworkspacedata"}, "Xcode project (MyApp.xcodeproj)", "ios", projectClear},

		{"js-backend", []string{"package.json:{\"dependencies\":{\"express\":\"4\"}}"}, "JavaScript project (not a mobile app)", "", projectNonMobile},

		{"nested-js-and-android", []string{"package.json:{\"dependencies\":{\"express\":\"4\"}}", "settings.gradle"}, "multiple projects detected (JavaScript project, Android project)", "", projectAmbiguous},
		{"nested-ios-and-android", []string{"MyApp.xcodeproj/project.pbxproj", "settings.gradle"}, "multiple projects detected (Xcode project (MyApp.xcodeproj), Android project)", "", projectAmbiguous},

		{"empty", nil, "no app project detected", "", projectNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, f := range tc.files {
				path, content := f, "x"
				if i := indexByte(f, ':'); i >= 0 {
					path, content = f[:i], f[i+1:]
				}
				full := filepath.Join(dir, path)
				if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			label, platform, status := detectAppProject(dir)
			if label != tc.wantLabel || platform != tc.wantPlatform || status != tc.wantStatus {
				t.Fatalf("detectAppProject = %q,%q,%d want %q,%q,%d",
					label, platform, status, tc.wantLabel, tc.wantPlatform, tc.wantStatus)
			}
		})
	}
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func TestPlatformFromLabel(t *testing.T) {
	cases := map[string]string{
		"Xcode project (App.xcodeproj)": "ios",
		"Tuist project (iOS)":           "ios",
		"Swift package":                 "ios",
		"Android project":               "android",
		"Flutter app":                   "cross",
		"React Native app":              "cross",
		"no app project detected":       "unknown",
	}
	for label, want := range cases {
		if got := platformFromLabel(label); got != want {
			t.Errorf("platformFromLabel(%q) = %q, want %q", label, got, want)
		}
	}
}

func TestSetupProjectNote(t *testing.T) {
	if note := setupProjectNote(projectClear); note != "" {
		t.Errorf("clear project should add no note, got %q", note)
	}
	for _, status := range []projectStatus{projectAmbiguous, projectNonMobile, projectNone} {
		note := setupProjectNote(status)
		if note == "" {
			t.Errorf("status %d should hand the platform decision to the agent, got empty note", status)
		}
		if !strings.Contains(note, "Project detection:") {
			t.Errorf("status %d note missing detection prefix: %q", status, note)
		}
	}
}

func TestDetectBundleID(t *testing.T) {
	// Tuist: Project.swift with a bundleId field.
	tuist := t.TempDir()
	if err := os.WriteFile(filepath.Join(tuist, "Project.swift"),
		[]byte(`let project = Project(name: "App", targets: [.target(bundleId: "com.example.app")])`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := detectBundleID(tuist); got != "com.example.app" {
		t.Fatalf("Tuist bundle ID = %q, want com.example.app", got)
	}

	// Xcode: pbxproj, skipping the test targets.
	xcode := t.TempDir()
	proj := filepath.Join(xcode, "App.xcodeproj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	pbx := "PRODUCT_BUNDLE_IDENTIFIER = com.example.app.UITests;\nPRODUCT_BUNDLE_IDENTIFIER = com.example.app;\n"
	if err := os.WriteFile(filepath.Join(proj, "project.pbxproj"), []byte(pbx), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := detectBundleID(xcode); got != "com.example.app" {
		t.Fatalf("Xcode bundle ID = %q, want com.example.app (test target should be skipped)", got)
	}
}
