package api_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

func TestRawWrapsNonJSONErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("X-Request-Id", "req_123")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = w.Write([]byte("<html><body>Method Not Allowed</body></html>"))
	}))
	t.Cleanup(srv.Close)

	client := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	_, status, err := client.Raw(context.Background(), http.MethodPost, "/wrong", nil)
	if status != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", status)
	}
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %T %v", err, err)
	}
	if strings.Contains(apiErr.Message, "<html>") || !strings.Contains(apiErr.Message, "non-JSON HTTP 405") {
		t.Fatalf("message = %q", apiErr.Message)
	}
	if apiErr.RequestID != "req_123" {
		t.Fatalf("request ID = %q", apiErr.RequestID)
	}
}
