package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

func TestPackagesAttachProductsUsesAssociationBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/projects/proj/packages/pkg/actions/attach_products" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			Products []struct {
				ProductID           string `json:"product_id"`
				EligibilityCriteria string `json:"eligibility_criteria"`
			} `json:"products"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body.Products) != 2 || body.Products[0].ProductID != "prod_a" || body.Products[1].ProductID != "prod_b" {
			t.Fatalf("unexpected products: %+v", body.Products)
		}
		for _, product := range body.Products {
			if product.EligibilityCriteria != "all" {
				t.Fatalf("eligibility = %q, want all", product.EligibilityCriteria)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	client := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	if err := client.Packages.AttachProducts(context.Background(), "proj", "pkg", []string{"prod_a", "prod_b"}); err != nil {
		t.Fatal(err)
	}
}

func TestPackagesGetExpandsAndListsProductAssociations(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/projects/proj/packages/pkg":
			if r.URL.Query().Get("expand") != "product" {
				t.Fatalf("expand = %q, want product", r.URL.Query().Get("expand"))
			}
			_, _ = w.Write([]byte(`{"id":"pkg","lookup_key":"monthly","display_name":"Monthly","created_at":1,"object":"package","products":{"object":"list","url":"/products","next_page":null,"items":[]}}`))
		case "/projects/proj/packages/pkg/products":
			_, _ = w.Write([]byte(`{"object":"list","url":"/products","next_page":null,"items":[{"product":{"id":"prod","app_id":"app","created_at":1,"display_name":"Monthly","object":"product","state":"active","store_identifier":"monthly","type":"subscription"},"eligibility_criteria":"all"}]}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)

	client := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	if _, err := client.Packages.Get(context.Background(), "proj", "pkg"); err != nil {
		t.Fatal(err)
	}
	products, err := client.Packages.ListProducts(context.Background(), "proj", "pkg")
	if err != nil {
		t.Fatal(err)
	}
	if len(products.Items) != 1 || products.Items[0].Product.ID != "prod" || products.Items[0].EligibilityCriteria != api.All {
		t.Fatalf("unexpected associations: %+v", products.Items)
	}
}
