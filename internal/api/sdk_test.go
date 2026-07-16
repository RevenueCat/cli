package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

func TestSDKOfferingsUsesPublicKeyAndV1Path(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/v1/subscribers/user%2Fone/offerings" {
			t.Fatalf("path = %q", r.URL.EscapedPath())
		}
		if r.Header.Get("Authorization") != "Bearer test_public" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"current_offering_id":"default","offerings":[]}`))
	}))
	t.Cleanup(srv.Close)

	service := api.NewSDKService(srv.URL+"/v2", nil, "test")
	result, err := service.Offerings(context.Background(), "test_public", "user/one")
	if err != nil {
		t.Fatal(err)
	}
	if string(result) != `{"current_offering_id":"default","offerings":[]}` {
		t.Fatalf("result = %s", result)
	}
}

func TestSDKSimulatePurchasePostsReceipt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/receipts" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test_public" || r.Header.Get("X-Platform") != "iOS" {
			t.Fatalf("unexpected headers: %+v", r.Header)
		}
		var body api.SimulatedPurchase
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.FetchToken != "TEST_token" || body.AppUserID != "user" || body.ProductID != "monthly" || !body.SDKOriginated {
			t.Fatalf("unexpected body: %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"subscriber":{"entitlements":{"premium":{"expires_date":null}}}}`))
	}))
	t.Cleanup(srv.Close)

	service := api.NewSDKService(srv.URL+"/v2", nil, "test")
	result, err := service.SimulatePurchase(context.Background(), "test_public", api.SimulatedPurchase{
		FetchToken: "TEST_token", AppUserID: "user", ProductID: "monthly", InitiationSource: "purchase", SDKOriginated: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(result) != `{"subscriber":{"entitlements":{"premium":{"expires_date":null}}}}` {
		t.Fatalf("result = %s", result)
	}
}
