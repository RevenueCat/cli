package cli_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

func TestCustomerSimulatePurchaseUsesTestStoreReceiptFlow(t *testing.T) {
	var receipt api.SimulatedPurchase
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/projects/proj/products":
			if r.URL.Query().Get("app_id") != "app_test" {
				t.Fatalf("app_id = %q", r.URL.Query().Get("app_id"))
			}
			_, _ = io.WriteString(w, `{"object":"list","items":[{"id":"prod","app_id":"app_test","created_at":1,"display_name":"Monthly","object":"product","state":"active","store_identifier":"premium_monthly","type":"subscription"}]}`)
		case "/projects/proj/apps/app_test/public_api_keys":
			_, _ = io.WriteString(w, `{"object":"list","items":[{"id":"key","object":"public_api_key","app_id":"app_test","environment":"production","key":"test_public","created_at":1}]}`)
		case "/v1/receipts":
			if err := json.NewDecoder(r.Body).Decode(&receipt); err != nil {
				t.Fatal(err)
			}
			_, _ = io.WriteString(w, `{"subscriber":{"entitlements":{"premium":{"expires_date":null}}}}`)
		default:
			http.Error(w, "unexpected request: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	out, _, err := runProjectSetupCommand(t, server.URL,
		"customer", "simulate-purchase", "--app-id", "app_test", "--product", "premium_monthly", "--app-user-id", "demo-user", "--yes", "--json", "--no-input")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(receipt.FetchToken, "TEST_") || receipt.AppUserID != "demo-user" || receipt.ProductID != "premium_monthly" {
		t.Fatalf("unexpected receipt: %+v", receipt)
	}
	if !strings.Contains(out, `"premium"`) || !strings.Contains(out, `"fetch_token": "TEST_`) {
		t.Fatalf("unexpected output: %s", out)
	}
}
