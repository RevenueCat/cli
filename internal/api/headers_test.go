package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

// ExtraHeaders (sourced from RC_HEADERS) must ride on every request, including
// the Raw passthrough used by `rc api`.
func TestExtraHeadersSentOnEveryRequest(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-RC-Route")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	c := api.NewClient(api.Options{
		APIKey:       "sk_test",
		BaseURL:      srv.URL,
		ExtraHeaders: http.Header{"X-Rc-Route": []string{"canary-1"}},
	})

	if _, _, err := c.Raw(context.Background(), http.MethodGet, "/anything", nil); err != nil {
		t.Fatal(err)
	}
	if got != "canary-1" {
		t.Errorf("Raw: X-RC-Route = %q, want canary-1", got)
	}
}
