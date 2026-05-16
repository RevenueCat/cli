package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

func TestEntitlementsList_DecodesEmptyPage(t *testing.T) {
	srv := fixtureServer(t, map[string]string{
		"GET /projects/proj_test_001/entitlements": "projects_PROJ_entitlements.json",
	})
	c := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	page, err := c.Entitlements.List(context.Background(), "proj_test_001")
	if err != nil {
		t.Fatal(err)
	}
	if page.Object != "list" {
		t.Errorf("want object=list, got %q", page.Object)
	}
	if len(page.Items) != 0 {
		t.Errorf("expected empty page (fixture is empty), got %d items", len(page.Items))
	}
}

// TestEntitlementsCreate_PostsBodyShape verifies the create request body
// matches what the live API expects. Verified by a real round-trip on
// 2026-05-15 (lookup_key + display_name accepted, returned the expected shape).
func TestEntitlementsCreate_PostsBodyShape(t *testing.T) {
	var got struct {
		LookupKey   string `json:"lookup_key"`
		DisplayName string `json:"display_name"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/entitlements") {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		_, _ = w.Write([]byte(`{"id":"entl_x","lookup_key":"k","display_name":"d","object":"entitlement"}`))
	}))
	t.Cleanup(srv.Close)

	c := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	_, err := c.Entitlements.Create(context.Background(), "proj_x", api.EntitlementCreate{
		LookupKey: "k", DisplayName: "d",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.LookupKey != "k" || got.DisplayName != "d" {
		t.Errorf("body mismatch: %+v", got)
	}
}

func TestClient_BuildURL_QueryNotInPath(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"items":[],"object":"list"}`))
	}))
	t.Cleanup(srv.Close)
	c := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	_, err := c.Customers.List(context.Background(), "proj_x", &api.ListCustomersOptions{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(gotPath, "?") {
		t.Errorf("query leaked into path: %s", gotPath)
	}
	if gotQuery != "limit=5" {
		t.Errorf("want query limit=5, got %q", gotQuery)
	}
}
