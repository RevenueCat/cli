package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

func TestCanaryHeader(t *testing.T) {
	for name, canary := range map[string]string{"set": "my-canary", "unset": ""} {
		t.Run(name, func(t *testing.T) {
			var got string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Get("X-RC-Canary")
				w.Write([]byte(`{"items":[]}`))
			}))
			defer srv.Close()

			c := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL, Canary: canary})
			if _, err := c.Projects.List(context.Background()); err != nil {
				t.Fatal(err)
			}
			if got != canary {
				t.Errorf("X-RC-Canary = %q, want %q", got, canary)
			}
		})
	}
}
