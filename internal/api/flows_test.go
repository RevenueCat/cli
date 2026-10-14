package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

func TestFlowsList_SendsRepeatedTypeAndPagination(t *testing.T) {
	var gotPath string
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query()
		_, _ = w.Write([]byte(`{"object":"list","items":[{"id":"wf_1","object":"flow","name":"Onboarding","type":"paywall","created_at":1,"published_at":null,"has_unpublished_changes":true}],"next_page":null}`))
	}))
	t.Cleanup(srv.Close)

	c := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	page, err := c.Flows.List(context.Background(), "proj_x", api.FlowListOptions{
		ListOptions: api.ListOptions{Limit: 5, StartingAfter: "wf_0"},
		Type:        []string{"paywall", "web-funnel"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/projects/proj_x/flows" {
		t.Errorf("path = %q", gotPath)
	}
	if got := gotQuery["type"]; len(got) != 2 || got[0] != "paywall" || got[1] != "web-funnel" {
		t.Errorf("type = %v, want [paywall web-funnel]", got)
	}
	if gotQuery.Get("limit") != "5" || gotQuery.Get("starting_after") != "wf_0" {
		t.Errorf("pagination query = %v", gotQuery)
	}
	if f := page.Items[0]; f.PublishedAt != nil || !f.HasUnpublishedChanges {
		t.Errorf("publish state not decoded: %+v", f)
	}
}
