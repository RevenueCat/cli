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
			// A raw select in a command is how interactive-only decisions
			// sneak in with no flag and no --no-input path (the recurring
			// agent-can't-reach-it bug). Route branching choices through
			// decide()/requireID, which own the three-mode contract.
			name:   "branching choices only through decide / requireID",
			needle: "huh.NewSelect",
			roots:  []string{"internal/tui"},
			allow: map[string]string{
				"decide.go":  "the decide() primitive itself",
				"resolve.go": "the requireID picker itself",
				"rico.go":    "altscreen resume picker",
				"setup.go":   "agent picker + autonomy: launch-time choices with no server side, flags tracked separately",
				// Pre-decide() backlog: each is flag-guarded (the field is
				// skipped when a flag set the value) so agents already have a
				// path; migrate to decide() opportunistically.
				"apps.go":       "app type; flag-guarded, pre-decide backlog",
				"apps_apple.go": "ASC team pick; flag-guarded (--team-id), pre-decide backlog",
				"auth.go":       "login method + product updates; flag-guarded, pre-decide backlog",
				"customers.go":  "grant duration + entity picks; flag-guarded, pre-decide backlog",
				"products.go":   "product type; flag-guarded, pre-decide backlog",
				"projects.go":   "project pick; flag-guarded, pre-decide backlog",
			},
			useThis: "use decide(rt, title, presetFromFlag, choices) so every branch has a flag and a --no-input path",
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
