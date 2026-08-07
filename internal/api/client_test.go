package api_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

func TestClient_SetsAuthAndHeaders(t *testing.T) {
	var gotAuth, gotUA, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotUA, gotAccept = r.Header.Get("Authorization"), r.Header.Get("User-Agent"), r.Header.Get("Accept")
		w.Write([]byte(`{"items":[{"id":"proj_1"}],"next_page":""}`))
	}))
	defer srv.Close()

	c := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL, UserAgent: "rc-test/1"})
	page, err := c.Projects.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(page.Items))
	}
	if gotAuth != "Bearer sk_test" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotUA != "rc-test/1" {
		t.Errorf("User-Agent = %q", gotUA)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q", gotAccept)
	}
}

func TestClient_ParsesAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "req_123")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"type":"resource_missing","message":"nope","doc_url":"https://errors.rev.cat/resource-missing","object":"error"}`))
	}))
	defer srv.Close()

	c := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	_, err := c.Projects.List(context.Background())
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want *api.APIError, got %T: %v", err, err)
	}
	if apiErr.Status != 404 || apiErr.Type != "resource_missing" || apiErr.Message != "nope" {
		t.Errorf("apiErr = %+v", apiErr)
	}
	if apiErr.RequestID != "req_123" {
		t.Errorf("RequestID should come from the X-Request-Id header, got %q", apiErr.RequestID)
	}
	if apiErr.DocURL == "" {
		t.Error("doc_url should propagate from the body")
	}
}

func TestClient_NonJSONErrorBodyFallsBack(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("upstream on fire"))
	}))
	defer srv.Close()

	c := api.NewClient(api.Options{APIKey: "sk", BaseURL: srv.URL})
	_, err := c.Projects.List(context.Background())
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want *api.APIError, got %T", err)
	}
	if apiErr.Status != 500 || apiErr.Type != "http_error" {
		t.Errorf("want 500/http_error, got %d/%s", apiErr.Status, apiErr.Type)
	}
	if apiErr.Message == "" {
		t.Error("a non-JSON error body should still yield a synthetic message")
	}
}

// A repeat GET sends If-None-Match; a 304 must be served from the cached body,
// not surfaced as an error.
func TestClient_ETagCachingServes304(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("If-None-Match") == "etag-1" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", "etag-1")
		w.Write([]byte(`{"items":[{"id":"proj_1"}],"next_page":""}`))
	}))
	defer srv.Close()

	c := api.NewClient(api.Options{APIKey: "sk", BaseURL: srv.URL})
	if _, err := c.Projects.List(context.Background()); err != nil {
		t.Fatal(err)
	}
	second, err := c.Projects.List(context.Background())
	if err != nil {
		t.Fatalf("304 should serve from cache, not error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 server round-trips, got %d", calls)
	}
	if len(second.Items) != 1 {
		t.Fatalf("cached body should decode to 1 item, got %d", len(second.Items))
	}
}

func TestClient_RespectsContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := api.NewClient(api.Options{APIKey: "sk", BaseURL: srv.URL})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Projects.List(ctx); err == nil {
		t.Fatal("expected an error from a canceled context")
	}
}
