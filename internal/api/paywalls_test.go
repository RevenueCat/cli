package api_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestPaywallsSetOffering(t *testing.T) {
	ofrng := "ofrng_x"
	tests := []struct {
		name       string
		offeringID *string
		wantBody   string
		response   string
		wantResult string
	}{
		{
			name:       "attach",
			offeringID: &ofrng,
			wantBody:   `{"offering_id":"ofrng_x","revision":7}`,
			response:   `{"id":"pw","offering_id":"ofrng_x","created_at":1,"published_at":null,"object":"paywall"}`,
			wantResult: "ofrng_x",
		},
		{
			name:       "detach",
			offeringID: nil,
			wantBody:   `{"offering_id":null,"revision":7}`,
			response:   `{"id":"pw","offering_id":null,"created_at":1,"published_at":null,"object":"paywall"}`,
			wantResult: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPatch || r.URL.Path != "/projects/proj/paywalls/pw" {
					t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				body, _ := io.ReadAll(r.Body)
				if got := strings.TrimSpace(string(body)); got != tt.wantBody {
					t.Fatalf("body = %s, want %s", got, tt.wantBody)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.response))
			}))
			t.Cleanup(srv.Close)

			client := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
			paywall, err := client.Paywalls.SetOffering(context.Background(), "proj", "pw", 7, tt.offeringID)
			if err != nil {
				t.Fatal(err)
			}
			if paywall.OfferingID != tt.wantResult {
				t.Fatalf("offering_id = %q, want %q", paywall.OfferingID, tt.wantResult)
			}
		})
	}
}

func TestPaywallsUnpublishPreservesDraftState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/projects/proj/paywalls/pw/actions/unpublish" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"pw","name":"Default","offering_id":"ofrng","created_at":1,"published_at":null,"object":"paywall"}`))
	}))
	t.Cleanup(srv.Close)

	client := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	paywall, err := client.Paywalls.Unpublish(context.Background(), "proj", "pw")
	if err != nil {
		t.Fatal(err)
	}
	if paywall.PublishedAt != nil {
		t.Fatalf("published_at = %v, want nil", paywall.PublishedAt)
	}
}
