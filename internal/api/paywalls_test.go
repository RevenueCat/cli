package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

// An explicit "state_declarations": null clears the stored declarations
// server-side, so an unset field must be omitted from the PATCH entirely.
func TestPaywallDraftUpdateOmitsUnsetStateDeclarations(t *testing.T) {
	update := api.PaywallDraftUpdate{
		Revision:                1,
		ComponentsConfig:        json.RawMessage(`{}`),
		ComponentsLocalizations: json.RawMessage(`{}`),
		DefaultLocale:           "en_US",
	}
	unset, err := json.Marshal(update)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(unset), "state_declarations") {
		t.Fatalf("unset state_declarations must be omitted, not sent as null: %s", unset)
	}

	update.StateDeclarations = json.RawMessage(`{}`)
	set, err := json.Marshal(update)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(set), `"state_declarations":{}`) {
		t.Fatalf("set state_declarations missing from PATCH body: %s", set)
	}
}

func TestPaywallsPublishPreservesPublishedState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/projects/proj/paywalls/pw/actions/publish" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"pw","name":"Default","offering_id":"ofrng","created_at":1,"published_at":2,"object":"paywall"}`))
	}))
	t.Cleanup(srv.Close)

	client := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	paywall, err := client.Paywalls.Publish(context.Background(), "proj", "pw")
	if err != nil {
		t.Fatal(err)
	}
	if paywall.PublishedAt == nil || *paywall.PublishedAt != 2 {
		t.Fatalf("published_at = %v, want 2", paywall.PublishedAt)
	}
}

func TestPaywallsSetOffering(t *testing.T) {
	ofrng := "ofrng_x"
	tests := []struct {
		name       string
		offeringID *string
		wantBody   string
		response   string
		wantResult string
	}{
		{
			name:       "attach",
			offeringID: &ofrng,
			wantBody:   `{"offering_id":"ofrng_x","revision":7}`,
			response:   `{"id":"pw","offering_id":"ofrng_x","created_at":1,"published_at":null,"object":"paywall"}`,
			wantResult: "ofrng_x",
		},
		{
			name:       "detach",
			offeringID: nil,
			wantBody:   `{"offering_id":null,"revision":7}`,
			response:   `{"id":"pw","offering_id":null,"created_at":1,"published_at":null,"object":"paywall"}`,
			wantResult: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPatch || r.URL.Path != "/projects/proj/paywalls/pw" {
					t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				body, _ := io.ReadAll(r.Body)
				if got := strings.TrimSpace(string(body)); got != tt.wantBody {
					t.Fatalf("body = %s, want %s", got, tt.wantBody)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.response))
			}))
			t.Cleanup(srv.Close)

			client := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
			paywall, err := client.Paywalls.SetOffering(context.Background(), "proj", "pw", 7, tt.offeringID)
			if err != nil {
				t.Fatal(err)
			}
			if paywall.OfferingID != tt.wantResult {
				t.Fatalf("offering_id = %q, want %q", paywall.OfferingID, tt.wantResult)
			}
		})
	}
}

func TestPaywallsGetGraph(t *testing.T) {
	tests := []struct {
		name       string
		fetch      func(client *api.Client) (*api.PaywallGraph, error)
		wantQuery  string
		response   string
		wantGraph  bool
		wantSteps  int
		wantScreen bool // first step's Paywall content present
	}{
		{
			name: "draft, no expand",
			fetch: func(client *api.Client) (*api.PaywallGraph, error) {
				return client.Paywalls.GetGraph(context.Background(), "proj", "pw", "draft")
			},
			wantQuery: "version=draft",
			response:  `{"object":"paywall_graph","id":"pw","version":"draft","graph":{"revision":3,"initial_step_id":"step_1","paywall_step_id":"step_1","total_steps":1,"steps":[{"id":"step_1","name":"Purchase","type":"screen","screen_types":["paywall"],"is_terminal":true,"paywall_id":"pw","edges":[],"unwired_triggers":[]}]}}`,
			wantGraph: true,
			wantSteps: 1,
		},
		{
			name: "published, expanded",
			fetch: func(client *api.Client) (*api.PaywallGraph, error) {
				return client.Paywalls.GetGraphWithScreenContent(context.Background(), "proj", "pw", "published")
			},
			wantQuery:  "version=published&expand=graph.steps.paywall",
			response:   `{"object":"paywall_graph","id":"pw","version":"published","graph":{"revision":3,"initial_step_id":"step_1","paywall_step_id":"step_1","total_steps":1,"steps":[{"id":"step_1","name":"Purchase","type":"screen","screen_types":["paywall"],"is_terminal":true,"paywall_id":"pw","edges":[],"unwired_triggers":[],"paywall":{"id":"pw","revision":5,"components_config":{},"components_localizations":{},"default_locale":"en_US","state_declarations":{}}}]}}`,
			wantGraph:  true,
			wantSteps:  1,
			wantScreen: true,
		},
		{
			name: "standalone paywall has a null graph",
			fetch: func(client *api.Client) (*api.PaywallGraph, error) {
				return client.Paywalls.GetGraph(context.Background(), "proj", "pw", "draft")
			},
			wantQuery: "version=draft",
			response:  `{"object":"paywall_graph","id":"pw","version":"draft","graph":null}`,
			wantGraph: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPath, gotQuery string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tt.response)
			}))
			t.Cleanup(srv.Close)

			client := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
			graph, err := tt.fetch(client)
			if err != nil {
				t.Fatal(err)
			}
			if gotPath != "/projects/proj/paywalls/pw/graph" {
				t.Fatalf("path = %s", gotPath)
			}
			if gotQuery != tt.wantQuery {
				t.Fatalf("query = %s, want %s", gotQuery, tt.wantQuery)
			}
			if (graph.Graph != nil) != tt.wantGraph {
				t.Fatalf("graph = %+v, want present=%v", graph.Graph, tt.wantGraph)
			}
			if !tt.wantGraph {
				return
			}
			if len(graph.Graph.Steps) != tt.wantSteps {
				t.Fatalf("steps = %d, want %d", len(graph.Graph.Steps), tt.wantSteps)
			}
			if (graph.Graph.Steps[0].Paywall != nil) != tt.wantScreen {
				t.Fatalf("step content present = %v, want %v", graph.Graph.Steps[0].Paywall != nil, tt.wantScreen)
			}
		})
	}
}

func TestPaywallsUpdateDraftStep(t *testing.T) {
	var gotPath, gotQuery string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		// The response id is the sibling's own canonical id, deliberately
		// different from the parent id in the path.
		_, _ = io.WriteString(w, `{"id":"pw_sibling","created_at":1,"published_at":null,"object":"paywall"}`)
	}))
	t.Cleanup(srv.Close)

	client := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	name := "should never be sent for a sibling"
	updated, err := client.Paywalls.UpdateDraftStep(context.Background(), "proj", "pw_parent", "step_2", api.PaywallDraftUpdate{
		Revision:                5,
		ComponentsConfig:        json.RawMessage(`{}`),
		ComponentsLocalizations: json.RawMessage(`{}`),
		DefaultLocale:           "en_US",
		Name:                    &name,
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/projects/proj/paywalls/pw_parent" {
		t.Fatalf("path = %s, want the parent's path, not the sibling's", gotPath)
	}
	if gotQuery != "step_id=step_2" {
		t.Fatalf("query = %s, want step_id=step_2", gotQuery)
	}
	if _, present := gotBody["name"]; present {
		t.Fatalf("body = %v, must never send name in selected mode", gotBody)
	}
	if updated.ID != "pw_sibling" {
		t.Fatalf("response id = %s, want pw_sibling (callers must read it from the response, not assume it equals the path id)", updated.ID)
	}
}

func TestPaywallsUnpublishPreservesDraftState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/projects/proj/paywalls/pw/actions/unpublish" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"pw","name":"Default","offering_id":"ofrng","created_at":1,"published_at":null,"object":"paywall"}`))
	}))
	t.Cleanup(srv.Close)

	client := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	paywall, err := client.Paywalls.Unpublish(context.Background(), "proj", "pw")
	if err != nil {
		t.Fatal(err)
	}
	if paywall.PublishedAt != nil {
		t.Fatalf("published_at = %v, want nil", paywall.PublishedAt)
	}
}
