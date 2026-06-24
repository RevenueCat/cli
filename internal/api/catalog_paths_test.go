package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

func TestCatalogLivePaths(t *testing.T) {
	tests := []struct {
		name     string
		method   string
		path     string
		response string
		call     func(*api.Client) error
	}{
		{
			name:     "product update",
			method:   http.MethodPost,
			path:     "/projects/proj/products/prod",
			response: `{"id":"prod","app_id":"app","created_at":1,"display_name":"Updated","object":"product","state":"active","store_identifier":"store_id","type":"subscription"}`,
			call: func(c *api.Client) error {
				name := "Updated"
				_, err := c.Products.Update(context.Background(), "proj", "prod", api.ProductUpdate{DisplayName: &name})
				return err
			},
		},
		{
			name:     "product create in store",
			method:   http.MethodPost,
			path:     "/projects/proj/products/prod/create_in_store",
			response: `{}`,
			call: func(c *api.Client) error {
				return c.Products.Push(context.Background(), "proj", "prod")
			},
		},
		{
			name:     "product list prices",
			method:   http.MethodGet,
			path:     "/projects/proj/products/prod/prices",
			response: `[{"amount_micros":9990000,"currency":"USD"}]`,
			call: func(c *api.Client) error {
				_, err := c.Products.ListPrices(context.Background(), "proj", "prod")
				return err
			},
		},
		{
			name:     "product add test store prices",
			method:   http.MethodPost,
			path:     "/projects/proj/products/prod/test_store_prices",
			response: `[{"amount_micros":9990000,"currency":"USD"}]`,
			call: func(c *api.Client) error {
				_, err := c.Products.AddTestStorePrices(context.Background(), "proj", "prod", []api.ProductPrice{
					{AmountMicros: 9_990_000, Currency: "USD"},
				})
				return err
			},
		},
		{
			name:     "product update price",
			method:   http.MethodPatch,
			path:     "/projects/proj/products/prod/prices/USD",
			response: `{"id":"price","amount_micros":12990000,"currency":"USD"}`,
			call: func(c *api.Client) error {
				_, err := c.Products.UpdatePrice(context.Background(), "proj", "prod", api.ProductPrice{
					AmountMicros: 12_990_000,
					Currency:     "USD",
				})
				return err
			},
		},
		{
			name:     "entitlement attach products",
			method:   http.MethodPost,
			path:     "/projects/proj/entitlements/ent/actions/attach_products",
			response: `{}`,
			call: func(c *api.Client) error {
				return c.Entitlements.AttachProducts(context.Background(), "proj", "ent", []string{"prod"})
			},
		},
		{
			name:     "entitlement detach products",
			method:   http.MethodPost,
			path:     "/projects/proj/entitlements/ent/actions/detach_products",
			response: `{}`,
			call: func(c *api.Client) error {
				return c.Entitlements.DetachProducts(context.Background(), "proj", "ent", []string{"prod"})
			},
		},
		{
			name:     "package get",
			method:   http.MethodGet,
			path:     "/projects/proj/packages/pkg",
			response: `{"id":"pkg","lookup_key":"monthly","display_name":"Monthly","created_at":1,"object":"package"}`,
			call: func(c *api.Client) error {
				_, err := c.Packages.Get(context.Background(), "proj", "pkg")
				return err
			},
		},
		{
			name:     "package update",
			method:   http.MethodPost,
			path:     "/projects/proj/packages/pkg",
			response: `{"id":"pkg","lookup_key":"monthly","display_name":"Updated","created_at":1,"object":"package"}`,
			call: func(c *api.Client) error {
				name := "Updated"
				_, err := c.Packages.Update(context.Background(), "proj", "pkg", api.PackageUpdate{DisplayName: &name})
				return err
			},
		},
		{
			name:     "package delete",
			method:   http.MethodDelete,
			path:     "/projects/proj/packages/pkg",
			response: `{}`,
			call: func(c *api.Client) error {
				return c.Packages.Delete(context.Background(), "proj", "pkg")
			},
		},
		{
			name:     "package products",
			method:   http.MethodGet,
			path:     "/projects/proj/packages/pkg/products",
			response: `{"object":"list","items":[]}`,
			call: func(c *api.Client) error {
				_, err := c.Packages.ListProducts(context.Background(), "proj", "pkg")
				return err
			},
		},
		{
			name:     "package attach products",
			method:   http.MethodPost,
			path:     "/projects/proj/packages/pkg/actions/attach_products",
			response: `{}`,
			call: func(c *api.Client) error {
				return c.Packages.AttachProducts(context.Background(), "proj", "pkg", []string{"prod"})
			},
		},
		{
			name:     "package detach products",
			method:   http.MethodPost,
			path:     "/projects/proj/packages/pkg/actions/detach_products",
			response: `{}`,
			call: func(c *api.Client) error {
				return c.Packages.DetachProducts(context.Background(), "proj", "pkg", []string{"prod"})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tt.method || r.URL.Path != tt.path {
					t.Fatalf("want %s %s, got %s %s", tt.method, tt.path, r.Method, r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.response))
			}))
			t.Cleanup(srv.Close)

			c := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
			if err := tt.call(c); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestProductsAddTestStorePrices_Body(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/projects/proj/products/prod/test_store_prices" {
			t.Fatalf("want POST /projects/proj/products/prod/test_store_prices, got %s %s", r.Method, r.URL.Path)
		}
		var got api.ProductPricesCreate
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if len(got.Prices) != 2 {
			t.Fatalf("want 2 prices, got %d", len(got.Prices))
		}
		if got.Prices[0].Currency != "USD" || got.Prices[0].AmountMicros != 9_990_000 {
			t.Fatalf("unexpected first price: %+v", got.Prices[0])
		}
		if got.Prices[1].Currency != "EUR" || got.Prices[1].AmountMicros != 8_990_000 {
			t.Fatalf("unexpected second price: %+v", got.Prices[1])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"amount_micros":9990000,"currency":"USD"},{"amount_micros":8990000,"currency":"EUR"}]`))
	}))
	t.Cleanup(srv.Close)

	c := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	out, err := c.Products.AddTestStorePrices(context.Background(), "proj", "prod", []api.ProductPrice{
		{AmountMicros: 9_990_000, Currency: "USD"},
		{AmountMicros: 8_990_000, Currency: "EUR"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("want 2 response prices, got %d", len(out))
	}
}

func TestProductsUpdatePrice_Body(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/projects/proj/products/prod/prices/USD" {
			t.Fatalf("want PATCH /projects/proj/products/prod/prices/USD, got %s %s", r.Method, r.URL.Path)
		}
		var got api.ProductPriceUpdate
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got.AmountMicros != 12_990_000 {
			t.Fatalf("unexpected update body: %+v", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"price","amount_micros":12990000,"currency":"USD"}`))
	}))
	t.Cleanup(srv.Close)

	c := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	out, err := c.Products.UpdatePrice(context.Background(), "proj", "prod", api.ProductPrice{
		AmountMicros: 12_990_000,
		Currency:     "USD",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Currency != "USD" || out.AmountMicros != 12_990_000 {
		t.Fatalf("unexpected response: %+v", out)
	}
}
