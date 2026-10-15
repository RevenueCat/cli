package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

func TestAudiencesGet_SendsEachExpandAsItsOwnParam(t *testing.T) {
	var gotPath string
	var gotExpand []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotExpand = r.URL.Query()["expand"]
		_, _ = w.Write([]byte(`{
			"id":"aud_1","object":"audience","name":"Pro users","customer_list_id":"cl_1",
			"rules":{"groups":[{"conditions":[{"field":"customAttribute:plan","operator":"is","value":"pro"}]}]},
			"created_at":1,
			"used_by":{"object":"audience_used_by","targeting_rules":[{"id":"rule_1","display_name":"Germany","state":"active"}],"experiments":[]}
		}`))
	}))
	t.Cleanup(srv.Close)

	c := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	a, err := c.Audiences.Get(context.Background(), "proj_x", "aud_1", api.AudienceExpandStats, api.AudienceExpandUsedBy)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/projects/proj_x/audiences/aud_1" {
		t.Errorf("path = %q", gotPath)
	}
	if len(gotExpand) != 2 || gotExpand[0] != "stats" || gotExpand[1] != "used_by" {
		t.Errorf("expand = %v, want [stats used_by]", gotExpand)
	}
	if a.Rules.Groups[0].Conditions[0].Value != "pro" {
		t.Errorf("rules not decoded: %+v", a.Rules)
	}
	if a.UsedBy == nil || a.UsedBy.TargetingRules[0].State != "active" {
		t.Errorf("used_by not decoded: %+v", a.UsedBy)
	}
}

func TestAudiencesGet_NoExpandSendsNoQuery(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"id":"aud_1","name":"All","rules":{"groups":[]}}`))
	}))
	t.Cleanup(srv.Close)

	c := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	if _, err := c.Audiences.Get(context.Background(), "proj_x", "aud_1"); err != nil {
		t.Fatal(err)
	}
	if gotQuery != "" {
		t.Errorf("query = %q, want empty", gotQuery)
	}
}
