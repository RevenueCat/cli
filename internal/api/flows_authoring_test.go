package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

func TestFlowAuthoringRoutes(t *testing.T) {
	published := api.DuplicateFlowSourceVersionPublished
	for _, tc := range []struct {
		name, method, suffix, body string
		call                       func(*api.Client) error
	}{
		{"draft", "GET", "/draft", "", func(c *api.Client) error {
			_, err := c.Flows.GetDraft(context.Background(), "proj", "wf_1")
			return err
		}},
		{"update", "PATCH", "/draft", `{"revision":3,"metadata":null,"deleted_step_ids":["old"]}`, func(c *api.Client) error {
			_, err := c.Flows.UpdateDraft(context.Background(), "proj", "wf_1", api.UpdateFlowDraft{FlowSettings: api.FlowSettings{Metadata: json.RawMessage("null")}, Revision: 3, DeletedStepIDs: []string{"old"}})
			return err
		}},
		{"duplicate", "POST", "/actions/duplicate", `{"source_version":"published"}`, func(c *api.Client) error {
			_, err := c.Flows.Duplicate(context.Background(), "proj", "wf_1", api.DuplicateFlow{SourceVersion: &published})
			return err
		}},
		{"validate latest", "POST", "/actions/validate", `{}`, func(c *api.Client) error {
			_, err := c.Flows.Validate(context.Background(), "proj", "wf_1", 0)
			return err
		}},
		{"validate revision", "POST", "/actions/validate", `{"revision":3}`, func(c *api.Client) error {
			_, err := c.Flows.Validate(context.Background(), "proj", "wf_1", 3)
			return err
		}},
		{"publish", "POST", "/actions/publish", `{"revision":3}`, func(c *api.Client) error {
			_, err := c.Flows.Publish(context.Background(), "proj", "wf_1", 3)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tc.method || r.URL.Path != "/projects/proj/flows/wf_1"+tc.suffix {
					t.Errorf("request = %s %s", r.Method, r.URL.Path)
				}
				body, _ := io.ReadAll(r.Body)
				if tc.body == "" {
					if len(body) != 0 {
						t.Errorf("unexpected body %s", body)
					}
				} else {
					var got, want any
					if err := json.Unmarshal(body, &got); err != nil {
						t.Fatal(err)
					}
					_ = json.Unmarshal([]byte(tc.body), &want)
					gotJSON, _ := json.Marshal(got)
					wantJSON, _ := json.Marshal(want)
					if string(gotJSON) != string(wantJSON) {
						t.Errorf("body = %s, want %s", gotJSON, wantJSON)
					}
				}
				_, _ = io.WriteString(w, `{"id":"wf_1","revision":3,"valid":true}`)
			}))
			defer server.Close()
			if err := tc.call(api.NewClient(api.Options{APIKey: "sk", BaseURL: server.URL})); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFlowUpdatePreservesRawScreenJSONAndConflict(t *testing.T) {
	const raw = `{"type":"screen","paywall":{"components_config":{"large":9007199254740993},"components_localizations":{"en_US":{"title":"Hello"}},"haptics_enabled":false}}`
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), raw) {
			t.Errorf("screen changed: %s", body)
		}
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"type":"resource_conflict","message":"Draft revision changed"}`)
	}))
	defer server.Close()
	client := api.NewClient(api.Options{APIKey: "sk", BaseURL: server.URL})
	_, err := client.Flows.UpdateDraft(context.Background(), "proj", "wf_1", api.UpdateFlowDraft{Revision: 2, UpdatedSteps: map[string]json.RawMessage{"screen": json.RawMessage(raw)}})
	var apiError *api.APIError
	if !errors.As(err, &apiError) || apiError.Status != http.StatusConflict {
		t.Fatalf("error = %v", err)
	}
	if requests != 1 {
		t.Errorf("conflicting update retried %d times", requests)
	}
}
