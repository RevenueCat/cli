package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

func TestPageNextCursor(t *testing.T) {
	cases := map[string]struct {
		nextPage string
		want     string
	}{
		"parses starting_after": {"https://api.revenuecat.com/v2/projects/p/fonts?starting_after=font_abc&limit=2", "font_abc"},
		"relative url":          {"/v2/projects/p/media_assets?starting_after=medas_x", "medas_x"},
		"no next page":          {"", ""},
		"no cursor param":       {"https://api.revenuecat.com/v2/projects/p/fonts", ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := api.Page[int]{NextPage: c.nextPage}.NextCursor()
			if got != c.want {
				t.Fatalf("NextCursor(%q) = %q, want %q", c.nextPage, got, c.want)
			}
		})
	}
}

// pagedServer serves pages keyed by the starting_after cursor ("" = first)
// and answers failCursor with a 400.
func pagedServer(t *testing.T, pages map[string]string, failCursor string) (*httptest.Server, *[]string) {
	t.Helper()
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		cursor := r.URL.Query().Get("starting_after")
		if cursor != "" && cursor == failCursor {
			http.Error(w, `{"message":"bad"}`, http.StatusBadRequest)
			return
		}
		body, ok := pages[cursor]
		if !ok {
			t.Errorf("unexpected cursor %q", cursor)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &queries
}

func TestListAllDrainsEveryPage(t *testing.T) {
	srv, queries := pagedServer(t, map[string]string{
		"":     `{"items":[{"id":"aud1"},{"id":"aud2"}],"next_page":"/v2/projects/p/audiences?starting_after=aud2"}`,
		"aud2": `{"items":[{"id":"aud3"}],"next_page":"/v2/projects/p/audiences?starting_after=aud3"}`,
		"aud3": `{"items":[{"id":"aud4"}]}`,
	}, "")
	c := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	page, err := c.Audiences.ListAll(context.Background(), "p")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, a := range page.Items {
		ids = append(ids, a.ID)
	}
	if got := strings.Join(ids, ","); got != "aud1,aud2,aud3,aud4" {
		t.Fatalf("ids = %s", got)
	}
	if page.NextPage != "" {
		t.Fatalf("NextPage = %q, want empty", page.NextPage)
	}
	if len(*queries) != 3 {
		t.Fatalf("requests = %v, want 3", *queries)
	}
}

func TestListAllSinglePage(t *testing.T) {
	srv, queries := pagedServer(t, map[string]string{
		"": `{"items":[{"id":"aud1"}]}`,
	}, "")
	c := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	page, err := c.Audiences.ListAll(context.Background(), "p")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || len(*queries) != 1 {
		t.Fatalf("items=%d requests=%v", len(page.Items), *queries)
	}
}

func TestListAllEmptyEncodesItemsAsArray(t *testing.T) {
	srv, _ := pagedServer(t, map[string]string{"": `{"items":[]}`}, "")
	c := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	page, err := c.Audiences.ListAll(context.Background(), "p")
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"items":[]`) {
		t.Fatalf("got %s", b)
	}
}

func TestListAllErrorOnLaterPage(t *testing.T) {
	srv, _ := pagedServer(t, map[string]string{
		"": `{"items":[{"id":"aud1"}],"next_page":"/v2/projects/p/audiences?starting_after=aud1"}`,
	}, "aud1")
	c := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	if page, err := c.Audiences.ListAll(context.Background(), "p"); err == nil {
		t.Fatalf("want error, got %d items", len(page.Items))
	}
}

func TestListAllKeepsFilterWhileDraining(t *testing.T) {
	srv, queries := pagedServer(t, map[string]string{
		"":      `{"items":[{"id":"flow1"}],"next_page":"/v2/projects/p/flows?type=paywall&starting_after=flow1"}`,
		"flow1": `{"items":[{"id":"flow2"}]}`,
	}, "")
	c := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	page, err := c.Flows.ListAll(context.Background(), "p", api.FlowListOptions{Type: []string{"paywall"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(page.Items))
	}
	want := []string{"type=paywall", "type=paywall&starting_after=flow1"}
	if !slices.Equal(*queries, want) {
		t.Fatalf("queries = %v, want %v", *queries, want)
	}
}

func TestListReturnsOnlyFirstPage(t *testing.T) {
	srv, queries := pagedServer(t, map[string]string{
		"": `{"items":[{"id":"aud1"}],"next_page":"/v2/projects/p/audiences?starting_after=aud1"}`,
	}, "")
	c := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	page, err := c.Audiences.List(context.Background(), "p")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.NextPage == "" || len(*queries) != 1 {
		t.Fatalf("items=%d next=%q requests=%v", len(page.Items), page.NextPage, *queries)
	}
}
