package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

func TestTargetingRuleRoutes(t *testing.T) {
	requests := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == "/projects/proj/targeting_rules" {
			var body api.TargetingRuleCreate
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.DisplayName != "US paywall" || body.OfferingID != "ofrng_us" || body.State != "inactive" {
				t.Fatalf("unexpected create body: %+v", body)
			}
		}
		if r.Method == http.MethodDelete {
			fmt.Fprint(w, `{"object":"deleted_object","id":"trle1"}`)
			return
		}
		if r.URL.Path == "/projects/proj/targeting_rules" && r.Method == http.MethodGet {
			fmt.Fprint(w, `{"object":"list","items":[{"object":"targeting_rule","id":"trle1","rule_type":"legacy","state":"inactive","display_name":"US paywall","offering_id":"ofrng_us"}],"next_page":null,"url":"/projects/proj/targeting_rules"}`)
			return
		}
		fmt.Fprint(w, `{"object":"targeting_rule","id":"trle1","rule_type":"legacy","state":"inactive","display_name":"US paywall","offering_id":"ofrng_us"}`)
	}))
	t.Cleanup(srv.Close)
	client := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	ctx := context.Background()
	page, err := client.TargetingRules.List(ctx, "proj", api.ListTargetingRulesOptions{State: "inactive", Limit: 5, StartingAfter: "trle0"})
	if err != nil || len(page.Items) != 1 || page.Items[0].OfferingID != "ofrng_us" {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	if _, err := client.TargetingRules.Get(ctx, "proj", "trle1"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.TargetingRules.Create(ctx, "proj", api.TargetingRuleCreate{DisplayName: "US paywall", OfferingID: "ofrng_us", State: "inactive"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.TargetingRules.Update(ctx, "proj", "trle1", api.TargetingRuleUpdate{"state": json.RawMessage(`"active"`)}); err != nil {
		t.Fatal(err)
	}
	if err := client.TargetingRules.Delete(ctx, "proj", "trle1"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"GET /projects/proj/targeting_rules?limit=5&starting_after=trle0&state=inactive",
		"GET /projects/proj/targeting_rules/trle1",
		"POST /projects/proj/targeting_rules",
		"POST /projects/proj/targeting_rules/trle1",
		"DELETE /projects/proj/targeting_rules/trle1",
	}
	if fmt.Sprint(requests) != fmt.Sprint(want) {
		t.Fatalf("requests = %v, want %v", requests, want)
	}
}
