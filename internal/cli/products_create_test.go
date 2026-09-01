package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

func runProductsCreate(t *testing.T, appType string, args ...string) (requests []string, createBody map[string]any, err error) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/proj/apps/app":
			_, _ = io.WriteString(w, `{"id":"app","name":"App","type":"`+appType+`","object":"app","project_id":"proj","created_at":1}`)
		case r.Method == http.MethodPost && r.URL.Path == "/projects/proj/products":
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &createBody)
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"prod_1","store_identifier":"sid","type":"one_time","app_id":"app","object":"product","created_at":1}`)
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	t.Setenv("RC_CONFIG_DIR", t.TempDir())
	t.Setenv("RC_BASE_URL", srv.URL)
	var out, errOut bytes.Buffer
	root := NewRootCmd("test")
	root.SetOut(&out)
	root.SetErr(&errOut)
	base := []string{"products", "create", "--api-key", "sk_test", "--project-id", "proj", "--no-input", "--json"}
	root.SetArgs(append(base, args...))
	err = root.ExecuteContext(context.Background())
	return requests, createBody, err
}

func postedProducts(requests []string) bool {
	for _, r := range requests {
		if r == "POST /projects/proj/products" {
			return true
		}
	}
	return false
}

func TestProductsCreate_AnyTypeReachesServer(t *testing.T) {
	cases := []struct {
		appType     string
		productType string
	}{
		{"test_store", "subscription"},
		{"test_store", "one_time"},
		{"app_store", "non_renewing_subscription"},
		{"play_store", "non_renewing_subscription"},
		{"amazon", "consumable"},
		{"stripe", "subscription"},
		{"roku", "one_time"},
		{"rc_billing", "subscription"},
	}
	for _, tc := range cases {
		t.Run(tc.appType+" "+tc.productType, func(t *testing.T) {
			args := []string{"--store-id", "sid", "--type", tc.productType, "--app-id", "app", "--title", "T"}
			if tc.appType == "test_store" && tc.productType == "subscription" {
				args = append(args, "--duration", "P1M")
			}
			requests, body, err := runProductsCreate(t, tc.appType, args...)
			if err != nil {
				t.Fatalf("create should defer to the server, got error: %v", err)
			}
			if !postedProducts(requests) {
				t.Fatalf("create did not reach the create endpoint: %v", requests)
			}
			if got, _ := body["type"].(string); got != tc.productType {
				t.Fatalf("outgoing type = %q, want %q", got, tc.productType)
			}
		})
	}
}

func TestProductTypeStoreSetsCoverEnum(t *testing.T) {
	stores := []api.AppType{
		api.AppType(api.TestStoreAppTypeTestStore),
		api.AppType(api.AppStoreAppTypeAppStore),
		api.AppType(api.PlayStoreAppTypePlayStore),
	}
	seen := map[api.ProductType]bool{}
	for _, s := range stores {
		for _, pt := range productTypesForStore(s) {
			if !pt.Valid() {
				t.Errorf("store %s offers invalid product type %q", s, pt)
			}
			seen[pt] = true
		}
	}
	enum := []api.ProductType{
		api.ProductTypeConsumable,
		api.ProductTypeNonConsumable,
		api.ProductTypeNonRenewingSubscription,
		api.ProductTypeOneTime,
		api.ProductTypeSubscription,
	}
	for _, pt := range enum {
		if !seen[pt] {
			t.Errorf("ProductType %q is in the catalog enum but no store offers it", pt)
		}
	}
	if len(seen) != len(enum) {
		t.Errorf("store sets offer %d distinct types, catalog enum has %d", len(seen), len(enum))
	}
}

func TestProductsCreate_DurationOutsideTestStoreSubscriptionReachesServer(t *testing.T) {
	for _, tc := range []struct{ appType, productType string }{
		{"app_store", "subscription"},
		{"test_store", "consumable"},
	} {
		requests, body, err := runProductsCreate(t, tc.appType,
			"--store-id", "sid", "--type", tc.productType, "--app-id", "app", "--title", "T", "--duration", "P1M")
		if err != nil {
			t.Fatalf("%s %s: create should defer to the server, got: %v", tc.appType, tc.productType, err)
		}
		if !postedProducts(requests) {
			t.Fatalf("%s %s: create did not reach the server: %v", tc.appType, tc.productType, requests)
		}
		sub, ok := body["subscription"].(map[string]any)
		if !ok || sub["duration"] != "P1M" {
			t.Fatalf("%s %s: outgoing subscription duration missing, body: %v", tc.appType, tc.productType, body)
		}
	}
}

func TestProductsCreate_DurationAcceptedOnTestStore(t *testing.T) {
	requests, body, err := runProductsCreate(t, "test_store",
		"--store-id", "sid", "--type", "subscription", "--app-id", "app", "--title", "T", "--duration", "P1M")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !postedProducts(requests) {
		t.Fatalf("create did not reach the server: %v", requests)
	}
	sub, ok := body["subscription"].(map[string]any)
	if !ok {
		t.Fatalf("outgoing body missing subscription: %v", body)
	}
	if sub["duration"] != "P1M" {
		t.Fatalf("outgoing duration = %v, want P1M", sub["duration"])
	}
}

func TestProductsCreate_DurationRequiredForTestStoreSubscription(t *testing.T) {
	requests, _, err := runProductsCreate(t, "test_store",
		"--store-id", "sid", "--type", "subscription", "--app-id", "app", "--title", "T")
	if err == nil {
		t.Fatal("expected client-side rejection for missing --duration on a Test Store subscription")
	}
	if !strings.Contains(err.Error(), "--duration") {
		t.Fatalf("error should mention --duration, got: %v", err)
	}
	if postedProducts(requests) {
		t.Fatalf("missing --duration still reached the server: %v", requests)
	}
}

func TestProductsCreate_TypeRequiredWhenNonInteractive(t *testing.T) {
	requests, _, err := runProductsCreate(t, "app_store",
		"--store-id", "sid", "--app-id", "app")
	if err == nil {
		t.Fatal("expected client-side rejection for missing --type when non-interactive")
	}
	if !strings.Contains(err.Error(), "--type is required") {
		t.Fatalf("error should mention --type is required, got: %v", err)
	}
	if postedProducts(requests) {
		t.Fatalf("missing --type still reached the create endpoint: %v", requests)
	}
}

func TestProductsCreate_TitleNotRequiredForNonTestStore(t *testing.T) {
	requests, _, err := runProductsCreate(t, "app_store",
		"--store-id", "sid", "--type", "one_time", "--app-id", "app")
	if err != nil {
		t.Fatalf("unexpected error creating a non-Test-store product without --title: %v", err)
	}
	if !postedProducts(requests) {
		t.Fatalf("create did not reach the server: %v", requests)
	}
}

func TestProductsCreate_TitleRequiredForTestStore(t *testing.T) {
	requests, _, err := runProductsCreate(t, "test_store",
		"--store-id", "sid", "--type", "subscription", "--app-id", "app")
	if err == nil {
		t.Fatal("expected client-side rejection for missing --title on a Test Store app")
	}
	if !strings.Contains(err.Error(), "--title") {
		t.Fatalf("error should mention --title, got: %v", err)
	}
	if postedProducts(requests) {
		t.Fatalf("missing --title still reached the server: %v", requests)
	}
}
