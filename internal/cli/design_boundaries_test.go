package cli

// Design-system boundary enforcement. The design system only works if its
// layers cannot be bypassed, so this test turns the rules in
// docs/design-system.md into compile-adjacent failures:
//
//   - colors exist only in internal/output (brand.go) and internal/tui
//   - forms are built only through tui.Form / tui.Confirm (theme + spacing)
//   - consent is asked only through confirmOrAbort (--yes/--no-input contract)
//
// A new command that hand-rolls any of these fails here with a pointer to the
// primitive it should use. Deliberate exceptions are listed with reasons.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type boundaryRule struct {
	name    string
	needle  string
	roots   []string          // directories the needle is allowed in
	allow   map[string]string // extra file basename → reason
	useThis string
}

func TestDesignSystemBoundaries(t *testing.T) {
	rules := []boundaryRule{
		{
			name:    "colors only in the token/theme layers",
			needle:  "charmbracelet/lipgloss",
			roots:   []string{"internal/output", "internal/tui"},
			useThis: "use output.Style* / brand tokens instead of picking colors",
		},
		{
			name:   "forms only through the themed tui layer",
			needle: "huh.NewForm(",
			roots:  []string{"internal/tui"},
			allow: map[string]string{
				"rico.go": "altscreen resume picker owns its full-screen theme",
			},
			useThis: "use tui.Form(...).Run() so theme and spacing apply",
		},
		{
			name:   "consent only through confirmOrAbort",
			needle: "Globals.AssumeYes",
			roots:  []string{},
			allow: map[string]string{
				"root.go":       "flag definition",
				"confirm.go":    "the primitive itself",
				"skills.go":     "install-consent policy check, not a prompt",
				"apps_apple.go": "guided-flow prompt-eligibility checks",
				"rico.go":       "tool-approval policy for destructive agent actions",
			},
			useThis: "use confirmOrAbort(rt, msg) so --yes/--no-input behave uniformly",
		},
		{
			name:   "prompt-gate only through rt.CanPrompt",
			needle: "tui.IsInteractive(",
			roots:  []string{"internal/tui"},
			allow: map[string]string{
				"runtime.go":     "CanPrompt is the one true prompt-gate",
				"apps_apple.go":  "output-only gates, suppressed under --json",
				"paywalls_ai.go": "self-gating requireProject/requireID branch",
				"open.go":        "guarded by an IsJSON early-return above",
				"decide.go":      "caller-guarded prompt primitive",
			},
			useThis: "use rt.CanPrompt() instead of hand-rolling the --json/--no-input/TTY check",
		},
	}

	repoRoot := filepath.Join("..", "..")
	for _, rule := range rules {
		t.Run(rule.name, func(t *testing.T) {
			err := filepath.Walk(filepath.Join(repoRoot, "internal"), func(path string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
					return err
				}
				rel, _ := filepath.Rel(repoRoot, path)
				for _, root := range rule.roots {
					if strings.HasPrefix(filepath.ToSlash(rel), root+"/") {
						return nil
					}
				}
				if _, ok := rule.allow[filepath.Base(path)]; ok {
					return nil
				}
				data, readErr := os.ReadFile(path)
				if readErr != nil {
					return readErr
				}
				if strings.Contains(string(data), rule.needle) {
					t.Errorf("%s: contains %q — %s (deliberate exceptions go in the allow list with a reason)", rel, rule.needle, rule.useThis)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
