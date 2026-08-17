package cli

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/config"
	"github.com/revenuecat/cli/internal/output"
	"github.com/spf13/cobra"
)

func TestDetectAppProject(t *testing.T) {
	cases := []struct {
		name         string
		files        []string
		wantLabel    string
		wantPlatform string
		wantStatus   projectStatus
		wantAppDir   string
	}{
		{"xcode", []string{"MyApp.xcodeproj/project.pbxproj"}, "Xcode project (MyApp.xcodeproj)", "ios", projectClear, ""},
		{"flutter", []string{"pubspec.yaml"}, "Flutter app", "cross", projectClear, ""},
		{"react-native", []string{"package.json:{\"dependencies\":{\"react-native\":\"0.74\"}}"}, "React Native app", "cross", projectClear, ""},
		{"android", []string{"settings.gradle"}, "Android project", "android", projectClear, ""},
		{"tuist", []string{"Project.swift"}, "Tuist project (iOS)", "ios", projectClear, ""},
		// A CocoaPods iOS project surfaces twice but is still one clear app.
		{"xcode-with-workspace", []string{"MyApp.xcodeproj/project.pbxproj", "MyApp.xcworkspace/contents.xcworkspacedata"}, "Xcode project (MyApp.xcodeproj)", "ios", projectClear, ""},

		{"js-backend", []string{"package.json:{\"dependencies\":{\"express\":\"4\"}}"}, "JavaScript project (not a mobile app)", "", projectNonMobile, ""},

		// A mobile app carrying a tooling package.json is still one clear app.
		{"ios-with-tooling-package-json", []string{"MyApp.xcodeproj/project.pbxproj", "package.json:{\"dependencies\":{\"prettier\":\"3\"}}"}, "Xcode project (MyApp.xcodeproj)", "ios", projectClear, ""},
		{"android-with-tooling-package-json", []string{"package.json:{\"dependencies\":{\"express\":\"4\"}}", "settings.gradle"}, "Android project", "android", projectClear, ""},

		{"nested-ios-and-android", []string{"MyApp.xcodeproj/project.pbxproj", "settings.gradle"}, "multiple projects detected (Xcode project (MyApp.xcodeproj), Android project)", "", projectAmbiguous, ""},

		// No markers at the root, but a single app one level down is picked up.
		{"single-app-in-subdir", []string{"ios/MyApp.xcodeproj/project.pbxproj"}, "Xcode project (MyApp.xcodeproj) (in ./ios)", "ios", projectClear, "ios"},
		{"two-apps-in-subdirs", []string{"android-app/settings.gradle", "ios-app/MyApp.xcodeproj/project.pbxproj"}, "multiple app projects in subdirectories (Android project (./android-app), Xcode project (MyApp.xcodeproj) (./ios-app))", "", projectAmbiguous, ""},
		{"subdir-scan-skips-dependencies", []string{"node_modules/pubspec.yaml"}, "no app project detected", "", projectNone, ""},

		{"empty", nil, "no app project detected", "", projectNone, ""},
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
			label, platform, status, appDir := detectAppProject(dir)
			if label != tc.wantLabel || platform != tc.wantPlatform || status != tc.wantStatus || appDir != tc.wantAppDir {
				t.Fatalf("detectAppProject = %q,%q,%d,%q want %q,%q,%d,%q",
					label, platform, status, appDir, tc.wantLabel, tc.wantPlatform, tc.wantStatus, tc.wantAppDir)
			}
		})
	}
}

func TestDetectSetupStage_EmptyPlatformDoesNotForceApple(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/apps"):
			_, _ = io.WriteString(w, `{"items":[]}`)
		case strings.HasSuffix(r.URL.Path, "/offerings"):
			_, _ = io.WriteString(w, `{"items":[{"id":"ofrng_x"}]}`)
		case strings.HasSuffix(r.URL.Path, "/products"):
			_, _ = io.WriteString(w, `{"items":[]}`)
		default:
			http.Error(w, "unexpected "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	rt := &Runtime{
		Globals: &Globals{NoInput: true, Version: "test"},
		Config:  &config.Config{APIKey: "sk_test", ProjectID: "proj", BaseURL: srv.URL},
		Out:     output.NewRenderer(io.Discard, io.Discard, false, false, false, ""),
		client:  api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL}),
	}
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	// A Test Store exists but no store apps do. An empty platform must defer to
	// the agent, not route to Apple; a clear iOS platform still connects Apple.
	if stage := detectSetupStage(cmd, rt, ""); stage.PromptID == "connect-apple" {
		t.Fatalf("empty platform routed to Apple: %+v", stage)
	}
	if stage := detectSetupStage(cmd, rt, "ios"); stage.PromptID != "connect-apple" {
		t.Fatalf("iOS platform should connect Apple, got %+v", stage)
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
	if note := setupProjectNote(projectClear, ""); note != "" {
		t.Errorf("clear project should add no note, got %q", note)
	}
	for _, status := range []projectStatus{projectAmbiguous, projectNonMobile, projectNone} {
		note := setupProjectNote(status, "")
		if note == "" {
			t.Errorf("status %d should hand the platform decision to the agent, got empty note", status)
		}
		if !strings.Contains(note, "Project detection:") {
			t.Errorf("status %d note missing detection prefix: %q", status, note)
		}
	}
	// A clear app in a subdirectory tells the agent where it is.
	note := setupProjectNote(projectClear, "ios")
	if !strings.Contains(note, "./ios") {
		t.Errorf("subdir note should point at ./ios, got %q", note)
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
