package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

func TestAppsUpdate_SendsAppleCredentialsWithoutBundleID(t *testing.T) {
	privateKey := "-----BEGIN PRIVATE KEY-----\nsecret\n-----END PRIVATE KEY-----\n"
	keyID := "KEY123"
	issuerID := "issuer-123"
	vendorNumber := "12345678"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/projects/proj/apps/app" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		appStore, ok := body["app_store"].(map[string]any)
		if !ok {
			t.Fatalf("missing app_store update: %#v", body)
		}
		if _, exists := appStore["bundle_id"]; exists {
			t.Errorf("partial credential update must not send bundle_id: %#v", appStore)
		}
		for field, want := range map[string]string{
			"subscription_private_key":         privateKey,
			"subscription_key_id":              keyID,
			"subscription_key_issuer":          issuerID,
			"app_store_connect_api_key":        privateKey,
			"app_store_connect_api_key_id":     keyID,
			"app_store_connect_api_key_issuer": issuerID,
			"app_store_connect_vendor_number":  vendorNumber,
		} {
			if got := appStore[field]; got != want {
				t.Errorf("%s: want %q, got %#v", field, want, got)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"app","name":"iOS","type":"app_store","created_at":1,"app_store":{"bundle_id":"com.example.app","subscription_key_configured":true,"app_store_connect_api_key_configured":true}}`))
	}))
	t.Cleanup(srv.Close)

	c := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	_, err := c.Apps.Update(context.Background(), "proj", "app", api.AppUpdate{AppStore: &api.AppStoreAppConfig{
		SubscriptionPrivateKey:      &privateKey,
		SubscriptionKeyID:           &keyID,
		SubscriptionKeyIssuer:       &issuerID,
		AppStoreConnectAPIKey:       &privateKey,
		AppStoreConnectAPIKeyID:     &keyID,
		AppStoreConnectAPIKeyIssuer: &issuerID,
		AppStoreConnectVendorNumber: &vendorNumber,
	}})
	if err != nil {
		t.Fatal(err)
	}
}

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
