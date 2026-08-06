package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

func TestExtraHeadersSentOnEveryRequest(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Example-Header")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	c := api.NewClient(api.Options{
		APIKey:       "sk_test",
		BaseURL:      srv.URL,
		ExtraHeaders: http.Header{"X-Example-Header": []string{"example-value"}},
	})

	if _, _, err := c.Raw(context.Background(), http.MethodGet, "/anything", nil); err != nil {
		t.Fatal(err)
	}
	if got != "example-value" {
		t.Errorf("Raw: X-Example-Header = %q, want example-value", got)
	}
}
