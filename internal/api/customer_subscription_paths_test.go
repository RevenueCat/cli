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

func TestCustomerAndSubscriptionLivePaths(t *testing.T) {
	tests := []struct {
		name     string
		method   string
		path     string
		response string
		call     func(*api.Client) error
		check    func(t *testing.T, body []byte)
	}{
		{
			name:     "customer revoke granted entitlement",
			method:   http.MethodPost,
			path:     "/projects/proj/customers/cust/actions/revoke_granted_entitlement",
			response: `{}`,
			call: func(c *api.Client) error {
				return c.Customers.RevokeEntitlement(context.Background(), "proj", "cust", "ent")
			},
		},
		{
			name:     "customer restore purchase by order id",
			method:   http.MethodPost,
			path:     "/projects/proj/customers/cust/actions/restore_purchase_by_order_id",
			response: `{}`,
			call: func(c *api.Client) error {
				return c.Customers.RestoreGooglePlay(context.Background(), "proj", "cust", "token")
			},
			check: func(t *testing.T, body []byte) {
				var got map[string]string
				if err := json.Unmarshal(body, &got); err != nil {
					t.Fatal(err)
				}
				if got["fetch_token"] != "token" {
					t.Fatalf("want fetch_token body, got %s", string(body))
				}
			},
		},
		{
			name:     "customer wallet update balance",
			method:   http.MethodPost,
			path:     "/projects/proj/customers/cust/virtual_currencies/update_balance",
			response: `{}`,
			call: func(c *api.Client) error {
				return c.Customers.WalletAdjustBalance(context.Background(), "proj", "cust", "GLD", 10)
			},
		},
		{
			name:     "subscription authenticated management url",
			method:   http.MethodGet,
			path:     "/projects/proj/subscriptions/sub/authenticated_management_url",
			response: `{"object":"authenticated_management_url","url":"https://example.com"}`,
			call: func(c *api.Client) error {
				_, err := c.Subscriptions.ManagementURL(context.Background(), "proj", "sub")
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body []byte
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tt.method || r.URL.Path != tt.path {
					t.Fatalf("want %s %s, got %s %s", tt.method, tt.path, r.Method, r.URL.Path)
				}
				var err error
				body, err = io.ReadAll(r.Body)
				if err != nil {
					t.Fatal(err)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.response))
			}))
			t.Cleanup(srv.Close)

			c := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
			if err := tt.call(c); err != nil {
				t.Fatal(err)
			}
			if tt.check != nil {
				tt.check(t, body)
			}
		})
	}
}
