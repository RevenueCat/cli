package api_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

// fixtureServer wires committed test fixtures (internal/api/testdata/v2)
// behind an httptest.Server. Each test reuses the helper to stay terse.
func fixtureServer(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		file, ok := routes[key]
		if !ok {
			t.Errorf("unexpected request: %s", key)
			http.NotFound(w, r)
			return
		}
		body, err := os.ReadFile(filepath.Join("testdata", "v2", file))
		if err != nil {
			t.Fatalf("read fixture: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestProjectsList(t *testing.T) {
	srv := fixtureServer(t, map[string]string{
		"GET /projects": "projects.json",
	})
	c := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	page, err := c.Projects.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("want 1 project, got %d", len(page.Items))
	}
	p := page.Items[0]
	if p.ID != "proj_test_001" || p.Name != "Test User" {
		t.Errorf("unexpected project: %+v", p)
	}
	if p.CreatedAt == 0 {
		t.Error("created_at not parsed")
	}
}

func TestProjectsGetWorkaround(t *testing.T) {
	srv := fixtureServer(t, map[string]string{
		"GET /projects": "projects.json",
	})
	c := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})

	got, err := c.Projects.Get(context.Background(), "proj_test_001")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "proj_test_001" {
		t.Errorf("want proj_test_001, got %s", got.ID)
	}

	_, err = c.Projects.Get(context.Background(), "missing")
	if err == nil {
		t.Fatal("want error for missing project")
	}
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) || apiErr.Type != "resource_missing" {
		t.Errorf("want resource_missing error, got %v", err)
	}
}

// List must follow next_page: an account with more projects than fit on one
// page should return them all, not just the first page.
func TestProjectsList_Paginates(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("starting_after") == "" {
			fmt.Fprintf(w, `{"items":[{"id":"proj_1"}],"next_page":%q}`, srv.URL+"/projects?starting_after=proj_1")
			return
		}
		fmt.Fprint(w, `{"items":[{"id":"proj_2"}]}`)
	}))
	defer srv.Close()

	c := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	page, err := c.Projects.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("want 2 projects across 2 pages, got %d", len(page.Items))
	}
	if page.Items[0].ID != "proj_1" || page.Items[1].ID != "proj_2" {
		t.Errorf("unexpected items: %+v", page.Items)
	}
}

// An invalid BaseURL must not panic: buildURL dereferences the parsed base, so
// a nil base would crash on the first request. The constructor falls back to a
// valid default instead.
func TestNewClient_InvalidBaseURLDoesNotPanic(t *testing.T) {
	c := api.NewClient(api.Options{APIKey: "x", BaseURL: "://not-a-url"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // fail fast at the transport, after buildURL has run
	if _, err := c.Projects.List(ctx); err == nil {
		t.Skip("request unexpectedly succeeded; test only guards against a panic")
	}
}
