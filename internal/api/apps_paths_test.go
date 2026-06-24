package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

func TestAppsStoreKitConfig_UsesLivePath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/projects/proj/apps/app/store_kit_config" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"store_kit_config_file","contents":{}}`))
	}))
	t.Cleanup(srv.Close)

	c := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	if _, err := c.Apps.StoreKitConfig(context.Background(), "proj", "app"); err != nil {
		t.Fatal(err)
	}
}
