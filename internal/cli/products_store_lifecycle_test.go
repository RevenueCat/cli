package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

func TestProductsStorePlan_ReadsJSONFromStdinAndPersistsPlan(t *testing.T) {
	var created api.StoreStatePlanCreate
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/projects/proj/apps/app":
			_, _ = io.WriteString(w, `{"id":"app","name":"iOS","type":"app_store","created_at":1,"app_store":{"bundle_id":"com.example"}}`)
		case "/projects/proj/store_state/plans":
			if err := json.NewDecoder(r.Body).Decode(&created); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"plan_123","object":"product_store_state_plan","status":"draft"}`)
		case "/projects/proj/store_state/plans/plan_123/actions/plan":
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"id":"plan_123","object":"product_store_state_plan","status":"plan_queued","polling_url":"/poll"}`)
		case "/projects/proj/store_state/plans/plan_123":
			_, _ = io.WriteString(w, plannedStoreStateJSON("planned", "pending"))
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	input := `{"desired_states":[{"store":"app_store","create_revenuecat_product":{"store_identifier":"com.example.pro","type":"subscription","display_name":"Pro","title":"Pro"},"common":{"duration":"P1M"}}]}`
	out, _, err := runStoreLifecycleCommand(t, server.URL, input,
		"products", "store", "plan", "app", "--file", "-", "--input-format", "json", "--json", "--no-input")
	if err != nil {
		t.Fatal(err)
	}
	if len(created.DesiredStates) != 1 || created.DesiredStates[0].CreateRevenueCatProduct.AppID != "app" {
		t.Fatalf("unexpected desired states: %+v", created.DesiredStates)
	}
	if !strings.Contains(out, `"id": "plan_123"`) {
		t.Fatalf("output does not contain persisted plan ID: %s", out)
	}
}

func TestProductsStoreApply_UsesExistingPlanWithoutRecreatingIt(t *testing.T) {
	requests := []string{}
	getCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/projects/proj/store_state/plans/plan_123":
			getCount++
			if getCount == 1 {
				_, _ = io.WriteString(w, plannedStoreStateJSON("planned", "pending"))
			} else {
				_, _ = io.WriteString(w, plannedStoreStateJSON("applied", "applied"))
			}
		case "/projects/proj/store_state/plans/plan_123/actions/apply":
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"id":"plan_123","object":"product_store_state_plan","status":"apply_queued","polling_url":"/poll"}`)
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	out, _, err := runStoreLifecycleCommand(t, server.URL, "",
		"products", "store", "apply", "plan_123", "--yes", "--json", "--no-input")
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range requests {
		if request == "POST /projects/proj/store_state/plans" {
			t.Fatalf("apply recreated desired state instead of using plan ID: %v", requests)
		}
	}
	if !strings.Contains(out, `"status": "applied"`) {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestProductsStoreApply_NoOpStillVerifiesReadiness(t *testing.T) {
	applied := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/projects/proj/store_state/plans/plan_123":
			_, _ = io.WriteString(w, `{"id":"plan_123","object":"product_store_state_plan","status":"planned_and_finished","has_changes":false,"actions":["discard"],"summary":{"products_added":0,"products_modified":0,"products_unchanged":1},"desired_states":[],"plan_items":[{"product_id":"prod_1","app_id":"app","store_identifier":"com.example.pro","action":"no_change","diff":[],"warnings":[],"error_message":null,"apply_status":null,"apply_error_message":null}],"error_message":null,"warnings":[]}`)
		case "/projects/proj/store_state/plans/plan_123/actions/apply":
			applied = true
			http.Error(w, "no-op plan must not apply", http.StatusInternalServerError)
		case "/projects/proj/products/prod_1/store_state":
			_, _ = io.WriteString(w, `{"project_id":"proj","product_id":"prod_1","store":"app_store","store_status":{"status":"needs_action","raw_store_status":"MISSING_METADATA"},"common":{},"store_state":{}}`)
		default:
			http.Error(w, "unexpected request "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	out, _, err := runStoreLifecycleCommand(t, server.URL, "",
		"products", "store", "apply", "plan_123", "--yes", "--json", "--no-input")
	if err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("no-op plan issued an apply request")
	}
	if !strings.Contains(out, `"overall": "INCOMPLETE"`) {
		t.Fatalf("no-op apply reported success instead of INCOMPLETE readiness: %s", out)
	}
	if !strings.Contains(out, `"raw_store_status": "MISSING_METADATA"`) {
		t.Fatalf("output missing raw store status: %s", out)
	}
}

func TestProductsStoreDiscard_RequiresYesUnderNoInput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "must not request without confirmation", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	_, _, err := runStoreLifecycleCommand(t, server.URL, "",
		"products", "store", "discard", "plan_123", "--json", "--no-input")
	if err == nil || !strings.Contains(err.Error(), "pass --yes") {
		t.Fatalf("error = %v, want --yes guidance", err)
	}
}

func TestProductsStorePlan_NoInputListsInputChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/projects/proj/apps/app" {
			_, _ = io.WriteString(w, `{"id":"app","name":"iOS","type":"app_store","created_at":1,"app_store":{"bundle_id":"com.example"}}`)
			return
		}
		http.Error(w, "unexpected request", http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	_, _, err := runStoreLifecycleCommand(t, server.URL, "",
		"products", "store", "plan", "app", "--json", "--no-input")
	if err == nil || !strings.Contains(err.Error(), "--file <path>") || !strings.Contains(err.Error(), "--file -") {
		t.Fatalf("error = %v, want deterministic input guidance", err)
	}
}

func TestProductsStoreApply_BlockerNeverMutates(t *testing.T) {
	mutated := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			mutated = true
		}
		plan := plannedStoreStateJSON("planned", "pending")
		plan = strings.Replace(plan, `"warnings":[],"error_message":null,"apply_status"`, `"warnings":[{"severity":"blocker","field":"common.title","message":"blocked"}],"error_message":null,"apply_status"`, 1)
		_, _ = io.WriteString(w, plan)
	}))
	t.Cleanup(server.Close)
	_, _, err := runStoreLifecycleCommand(t, server.URL, "",
		"products", "store", "apply", "plan_123", "--yes", "--json", "--no-input")
	if err == nil || !strings.Contains(err.Error(), "blocker warnings") {
		t.Fatalf("error = %v", err)
	}
	if mutated {
		t.Fatal("blocker plan made a mutating request")
	}
}

func TestWaitForStoreStatePlan_StopsOnUnexpectedTerminalStatus(t *testing.T) {
	service := staticStoreStatePlanService{plan: &api.StoreStatePlan{ID: "plan_123", Status: "expired"}}
	_, err := waitForStoreStatePlan(context.Background(), service, "proj", "plan_123", 0, planningFinished)
	if err == nil || !strings.Contains(err.Error(), "terminal status expired") {
		t.Fatalf("error = %v", err)
	}
}

func TestReadStoreStateJSON_RejectsAnotherApp(t *testing.T) {
	input := `[{"store":"app_store","create_revenuecat_product":{"app_id":"other","store_identifier":"pro","type":"subscription","display_name":"Pro","title":"Pro"}}]`
	_, err := readStoreStateJSON(strings.NewReader(input), "app")
	if err == nil || !strings.Contains(err.Error(), "command targets app") {
		t.Fatalf("error = %v", err)
	}
}

type staticStoreStatePlanService struct{ plan *api.StoreStatePlan }

func (s staticStoreStatePlanService) Get(context.Context, string, string) (*api.StoreStatePlan, error) {
	return s.plan, nil
}

func runStoreLifecycleCommand(t *testing.T, baseURL, input string, args ...string) (string, string, error) {
	t.Helper()
	t.Setenv("RC_CONFIG_DIR", t.TempDir())
	t.Setenv("RC_BASE_URL", baseURL)
	var stdout, stderr bytes.Buffer
	root := NewRootCmd("test")
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetIn(strings.NewReader(input))
	args = append(args, "--api-key", "sk_test", "--project-id", "proj")
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return stdout.String(), stderr.String(), err
}

func plannedStoreStateJSON(status, applyStatus string) string {
	return fmt.Sprintf(`{"id":"plan_123","object":"product_store_state_plan","status":%q,"has_changes":true,"actions":["apply","discard"],"summary":{"products_added":1,"products_modified":0,"products_unchanged":0},"desired_states":[],"plan_items":[{"product_id":null,"app_id":"app","store_identifier":"com.example.pro","action":"create","diff":[{"field":"common.title","from_value":null,"to_value":"Pro"}],"warnings":[],"error_message":null,"apply_status":%q,"apply_error_message":null}],"error_message":null,"warnings":[]}`, status, applyStatus)
}
