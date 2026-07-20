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
