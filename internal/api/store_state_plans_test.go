package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

func TestStoreStatePlanLifecyclePathsAndBodies(t *testing.T) {
	requests := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/projects/proj/store_state/plans":
			var body api.StoreStatePlanCreate
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if len(body.DesiredStates) != 1 || body.DesiredStates[0].CreateRevenueCatProduct.AppID != "app" {
				t.Fatalf("unexpected create body: %+v", body)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"plan","object":"product_store_state_plan","status":"draft"}`)
		case "/projects/proj/store_state/plans/plan":
			_, _ = io.WriteString(w, `{"id":"plan","object":"product_store_state_plan","status":"planned","has_changes":false,"actions":[],"summary":{"products_added":0,"products_modified":0,"products_unchanged":1},"desired_states":[],"plan_items":[],"error_message":null,"warnings":[]}`)
		default:
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
			_, _ = io.WriteString(w, `{"id":"plan","object":"product_store_state_plan","status":"queued","polling_url":"/poll"}`)
		}
	}))
	t.Cleanup(srv.Close)

	client := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	body := api.StoreStatePlanCreate{DesiredStates: []api.StoreStatePlanDesiredState{{
		Store: "app_store",
		CreateRevenueCatProduct: &api.StoreStatePlanProductCreate{
			AppID: "app", StoreIdentifier: "com.example.pro", Type: "subscription", DisplayName: "Pro", Title: "Pro",
		},
	}}}
	created, err := client.StoreStatePlans.Create(context.Background(), "proj", body)
	if err != nil || created.ID != "plan" {
		t.Fatalf("create: %+v, %v", created, err)
	}
	if _, err := client.StoreStatePlans.Plan(context.Background(), "proj", "plan"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.StoreStatePlans.Get(context.Background(), "proj", "plan"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.StoreStatePlans.Apply(context.Background(), "proj", "plan"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.StoreStatePlans.Discard(context.Background(), "proj", "plan"); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"POST /projects/proj/store_state/plans",
		"POST /projects/proj/store_state/plans/plan/actions/plan",
		"GET /projects/proj/store_state/plans/plan",
		"POST /projects/proj/store_state/plans/plan/actions/apply",
		"POST /projects/proj/store_state/plans/plan/actions/discard",
	}
	if !equalStringSlices(requests, want) {
		t.Fatalf("requests = %v, want %v", requests, want)
	}
}

func equalStringSlices(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
