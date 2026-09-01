package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProductsStoreSync_PlanOnlyNeverApplies(t *testing.T) {
	requests, stdout, stderr, err := runStoreSync(t, true)
	if err != nil {
		t.Fatalf("execute: %v\nstderr: %s", err, stderr)
	}
	if strings.Contains(strings.Join(requests, "\n"), "/actions/apply") {
		t.Fatalf("plan-only made apply request: %v", requests)
	}
	var result struct {
		Data struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("decode output: %v\n%s", err, stdout)
	}
	if result.Data.ID != "plan_123" || result.Data.Status != "planned" {
		t.Fatalf("unexpected output: %+v", result.Data)
	}
}

func TestProductsStoreSync_AppliesAfterExplicitYes(t *testing.T) {
	requests, stdout, stderr, err := runStoreSync(t, false)
	if err != nil {
		t.Fatalf("execute: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(strings.Join(requests, "\n"), "POST /projects/proj/store_state/plans/plan_123/actions/apply") {
		t.Fatalf("missing apply request: %v", requests)
	}
	var result struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("decode output: %v\n%s", err, stdout)
	}
	if result.Data.Status != "applied" {
		t.Fatalf("status = %q, want applied", result.Data.Status)
	}
}

func runStoreSync(t *testing.T, planOnly bool) (requests []string, stdout, stderr string, err error) {
	t.Helper()
	getCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/projects/proj/apps/app":
			_, _ = io.WriteString(w, `{"id":"app","name":"iOS","type":"app_store","created_at":1,"app_store":{"bundle_id":"com.example.app"}}`)
		case "/projects/proj/store_state/plans":
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"plan_123","object":"product_store_state_plan","status":"draft"}`)
		case "/projects/proj/store_state/plans/plan_123/actions/plan":
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"id":"plan_123","object":"product_store_state_plan","status":"plan_queued","polling_url":"/poll"}`)
		case "/projects/proj/store_state/plans/plan_123/actions/apply":
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"id":"plan_123","object":"product_store_state_plan","status":"apply_queued","polling_url":"/poll"}`)
		case "/projects/proj/store_state/plans/plan_123":
			getCount++
			status, applyStatus := "planned", "pending"
			if getCount > 1 {
				status, applyStatus = "applied", "applied"
			}
			_, _ = fmt.Fprintf(w, `{"id":"plan_123","object":"product_store_state_plan","status":%q,"has_changes":true,"actions":["apply","discard"],"summary":{"products_added":1,"products_modified":0,"products_unchanged":0},"desired_states":[],"plan_items":[{"product_id":null,"app_id":"app","store_identifier":"com.example.pro","action":"create","diff":[{"field":"common.title","from_value":null,"to_value":"Premium"}],"warnings":[],"error_message":null,"apply_status":%q,"apply_error_message":null}],"error_message":null,"warnings":[]}`, status, applyStatus)
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	file := filepath.Join(t.TempDir(), "catalog.csv")
	if writeErr := os.WriteFile(file, []byte("store,store_identifier,product_type,display_name,title,duration,territory,amount,currency,available\napp_store,com.example.pro,subscription,Pro,Premium,P1M,US,3.99,USD,true\n"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	t.Setenv("RC_CONFIG_DIR", t.TempDir())
	t.Setenv("RC_BASE_URL", srv.URL)
	var out, errOut bytes.Buffer
	root := NewRootCmd("test")
	root.SetOut(&out)
	root.SetErr(&errOut)
	args := []string{"products", "store", "sync", "app", "--file", file, "--api-key", "sk_test", "--project-id", "proj", "--no-input", "--yes", "--json"}
	if planOnly {
		args = append(args, "--plan-only")
	}
	root.SetArgs(args)
	err = root.ExecuteContext(context.Background())
	return requests, out.String(), errOut.String(), err
}

func TestReadStoreStateJSON_AcceptsAllPlanStores(t *testing.T) {
	for _, store := range []string{"app_store", "play_store", "rc_billing", "test_store"} {
		in := strings.NewReader(`{"desired_states":[{"store":"` + store + `","create_revenuecat_product":{"app_id":"app_x","store_identifier":"sid","type":"subscription","display_name":"D","title":"T"}}]}`)
		states, err := readStoreStateJSON(in, "app_x")
		if err != nil {
			t.Fatalf("store %s should parse (the server owns the supported set): %v", store, err)
		}
		if len(states) != 1 || states[0].Store != store {
			t.Fatalf("store %s: unexpected states %+v", store, states)
		}
	}
}
