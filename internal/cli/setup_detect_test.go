package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectAppProject(t *testing.T) {
	cases := []struct {
		name  string
		files []string
		want  string
		ok    bool
	}{
		{"xcode", []string{"MyApp.xcodeproj/project.pbxproj"}, "Xcode project (MyApp.xcodeproj)", true},
		{"flutter", []string{"pubspec.yaml"}, "Flutter app", true},
		{"react-native", []string{"package.json:{\"dependencies\":{\"react-native\":\"0.74\"}}"}, "React Native app", true},
		{"android", []string{"settings.gradle"}, "Android project", true},
		{"tuist", []string{"Project.swift"}, "Tuist project (iOS)", true},
		{"empty", nil, "no app project detected", false},
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
			label, ok := detectAppProject(dir)
			if label != tc.want || ok != tc.ok {
				t.Fatalf("detectAppProject = %q,%v want %q,%v", label, ok, tc.want, tc.ok)
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
