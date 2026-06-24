package api_test

import (
	"context"
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
