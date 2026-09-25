package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

func TestExperimentsReadRoutesAndFilters(t *testing.T) {
	requests := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/projects/proj/experiments":
			fmt.Fprint(w, `{"object":"list","items":[{"object":"experiment","id":"exp1","display_name":"Onboarding","status":"running","created_at":1,"updated_at":2,"offering_a":{"id":"ofrng_a"},"offering_b":{"id":"ofrng_b"}}],"next_page":null,"url":"/projects/proj/experiments"}`)
		case "/projects/proj/experiments/exp1":
			fmt.Fprint(w, `{"object":"experiment","id":"exp1","display_name":"Onboarding","status":"running","created_at":1,"updated_at":2}`)
		case "/projects/proj/experiments/exp1/results":
			fmt.Fprint(w, `{"object":"experiment_results","currency":"EUR","sections":[],"statistics":[],"predicted_ltv":null}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	client := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	ctx := context.Background()
	page, err := client.Experiments.List(ctx, "proj", api.ListExperimentsOptions{Status: "running", Limit: 5, StartingAfter: "exp0"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].OfferingB.ID != "ofrng_b" {
		t.Fatalf("unexpected page: %+v", page)
	}
	if _, err := client.Experiments.Get(ctx, "proj", "exp1"); err != nil {
		t.Fatal(err)
	}
	results, err := client.Experiments.Results(ctx, "proj", "exp1", api.ExperimentResultsOptions{Platform: "ios", Country: "US", ExposureStatus: "exposed", Currency: "EUR"})
	if err != nil {
		t.Fatal(err)
	}
	if results.Currency != "EUR" {
		t.Fatalf("currency = %q", results.Currency)
	}
	want := []string{
		"GET /projects/proj/experiments?limit=5&starting_after=exp0&status=running",
		"GET /projects/proj/experiments/exp1",
		"GET /projects/proj/experiments/exp1/results?country=US&currency=EUR&exposure_status=exposed&platform=ios",
	}
	if fmt.Sprint(requests) != fmt.Sprint(want) {
		t.Fatalf("requests = %v, want %v", requests, want)
	}
}
