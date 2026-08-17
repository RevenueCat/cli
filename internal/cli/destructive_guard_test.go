package cli_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/revenuecat/cli/internal/config"
)

// Reversible, soft state changes (archive/restore/set-current/enable/disable)
// are deliberately excluded — the codebase documents those as "no prompt".
var destructiveVerbs = map[string]bool{
	"delete":            true,
	"revoke":            true,
	"refund":            true,
	"cancel":            true,
	"transfer":          true,
	"grant":             true,
	"push":              true,
	"publish":           true,
	"unpublish":         true,
	"apply":             true,
	"discard":           true,
	"simulate-purchase": true,
}

func TestDestructiveCommands_RouteThroughConfirmOrAbort(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse package: %v", err)
	}

	// Which named functions ask for consent — directly or transitively — so a
	// command whose RunE delegates the prompt to a helper still counts as
	// guarded (e.g. `products store apply` → applyStoreStatePlan).
	guarding := guardingFuncs(pkgs)

	found := 0
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.CompositeLit)
				if !ok || !isCobraCommand(lit.Type) {
					return true
				}
				verb, runE := commandVerbAndRunE(lit)
				if !destructiveVerbs[verb] {
					return true
				}
				found++
				if !runEGuarded(runE, guarding) {
					pos := fset.Position(lit.Pos())
					t.Errorf("%s: destructive command %q reaches its action without confirmOrAbort — gate it via confirmOrAbort(rt, ...) so --yes/--no-input behave uniformly", pos, verb)
				}
				return true
			})
		}
	}
	if found == 0 {
		t.Fatal("no destructive commands found — the guard is not scanning the command definitions")
	}
}

// guardingFuncs returns the names of functions whose bodies reach confirmOrAbort,
// following one function calling another to a fixpoint.
func guardingFuncs(pkgs map[string]*ast.Package) map[string]bool {
	calls := map[string]map[string]bool{}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				calls[fn.Name.Name] = calledIdents(fn.Body)
			}
		}
	}
	guarding := map[string]bool{}
	for name, callees := range calls {
		if callees["confirmOrAbort"] {
			guarding[name] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for name, callees := range calls {
			if guarding[name] {
				continue
			}
			for callee := range callees {
				if guarding[callee] {
					guarding[name] = true
					changed = true
					break
				}
			}
		}
	}
	return guarding
}

func calledIdents(n ast.Node) map[string]bool {
	set := map[string]bool{}
	ast.Inspect(n, func(nn ast.Node) bool {
		if call, ok := nn.(*ast.CallExpr); ok {
			if id, ok := call.Fun.(*ast.Ident); ok {
				set[id.Name] = true
			}
		}
		return true
	})
	return set
}

func isCobraCommand(t ast.Expr) bool {
	sel, ok := t.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Command" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "cobra"
}

func commandVerbAndRunE(lit *ast.CompositeLit) (verb string, runE ast.Expr) {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch key.Name {
		case "Use":
			if bl, ok := kv.Value.(*ast.BasicLit); ok && bl.Kind == token.STRING {
				if use, err := strconv.Unquote(bl.Value); err == nil {
					if fields := strings.Fields(use); len(fields) > 0 {
						verb = fields[0]
					}
				}
			}
		case "RunE":
			runE = kv.Value
		}
	}
	return verb, runE
}

func runEGuarded(runE ast.Expr, guarding map[string]bool) bool {
	switch v := runE.(type) {
	case *ast.FuncLit:
		for callee := range calledIdents(v.Body) {
			if callee == "confirmOrAbort" || guarding[callee] {
				return true
			}
		}
	case *ast.Ident:
		return guarding[v.Name]
	}
	return false
}

// The fake server allows read-only GET preflight (some commands fetch state to
// decide whether extra confirmation is needed) but fails the test on any write,
// proving nothing is destroyed before consent.
func TestDestructiveCommands_RefuseUnderNoInputWithoutYes(t *testing.T) {
	commands := [][]string{
		{"apps", "delete", "app_x"},
		{"products", "delete", "prod_x"},
		{"products", "push", "prod_x"},
		{"paywalls", "delete", "pw_x"},
		{"paywalls", "publish", "pw_x"},
		{"paywalls", "unpublish", "pw_x"},
		{"offerings", "delete", "ofrng_x"},
		{"packages", "delete", "pkg_x"},
		{"entitlements", "delete", "ent_x"},
		{"webhooks", "delete", "wh_x"},
		{"purchases", "refund", "txn_x"},
		{"subscriptions", "cancel", "sub_x"},
		{"subscriptions", "refund", "sub_x"},
		{"products", "store", "discard", "plan_x"},
		{"customer", "revoke", "cust_x", "ent_x"},
		{"customer", "grant", "cust_x", "ent_x", "--duration", "monthly"},
		{"customer", "transfer", "cust_x", "--to", "cust_y"},
	}
	for _, args := range commands {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			configDir := t.TempDir()
			t.Setenv("RC_CONFIG_DIR", configDir)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("%s %s: state-changing call reached the network before confirmation", r.Method, r.URL.Path)
					http.Error(w, "unexpected write before confirmation", http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{}`))
			}))
			t.Cleanup(server.Close)
			if err := config.Save("", &config.Config{APIKey: "sk_test", ProjectID: "proj_test", BaseURL: server.URL}); err != nil {
				t.Fatal(err)
			}

			runArgs := append(append([]string{}, args...), "--no-input")
			_, _, err := runCmdInConfigDir(t, configDir, runArgs...)
			if err == nil {
				t.Fatal("want refusal under --no-input without --yes")
			}
			if !strings.Contains(err.Error(), "pass --yes") {
				t.Fatalf("want the confirmation gate error, got: %v", err)
			}
		})
	}
}

func TestDestructiveCommand_YesBypassesConfirmation(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("RC_CONFIG_DIR", configDir)
	var deleted bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/apps/app_x") {
			deleted = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		http.Error(w, "unexpected request", http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	if err := config.Save("", &config.Config{APIKey: "sk_test", ProjectID: "proj_test", BaseURL: server.URL}); err != nil {
		t.Fatal(err)
	}

	_, errb, err := runCmdInConfigDir(t, configDir, "apps", "delete", "app_x", "--yes", "--no-input")
	if err != nil {
		t.Fatalf("--yes should let the delete proceed: %v\nstderr: %s", err, errb)
	}
	if !deleted {
		t.Fatal("--yes did not let the command reach the store delete call")
	}
}
