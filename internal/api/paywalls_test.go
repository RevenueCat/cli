package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

func TestPaywallsPublishPreservesPublishedState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/projects/proj/paywalls/pw/actions/publish" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"pw","name":"Default","offering_id":"ofrng","created_at":1,"published_at":2,"object":"paywall"}`))
	}))
	t.Cleanup(srv.Close)

	client := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	paywall, err := client.Paywalls.Publish(context.Background(), "proj", "pw")
	if err != nil {
		t.Fatal(err)
	}
	if paywall.PublishedAt == nil || *paywall.PublishedAt != 2 {
		t.Fatalf("published_at = %v, want 2", paywall.PublishedAt)
	}
}
