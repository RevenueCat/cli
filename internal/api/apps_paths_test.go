package api_test

import (
	"context"
	"io"
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

func TestAppsPublicAPIKeys_ReturnsTypedKeys(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/projects/proj/apps/app/public_api_keys" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","items":[{"id":"key","object":"public_api_key","app_id":"app","environment":"production","key":"appl_test","created_at":1}],"next_page":null,"url":"/keys"}`)
	}))
	t.Cleanup(srv.Close)

	c := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	keys, err := c.Apps.PublicAPIKeys(context.Background(), "proj", "app")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys.Items) != 1 || keys.Items[0].Key != "appl_test" || keys.Items[0].Environment != "production" {
		t.Fatalf("unexpected keys: %+v", keys.Items)
	}
}
