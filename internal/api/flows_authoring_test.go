package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

func TestFlowAuthoringRoutes(t *testing.T) {
	published := api.DuplicateFlowSourceVersionPublished
	for _, tc := range []struct {
		name, method, suffix, body string
		call                       func(*api.Client) error
	}{
		{"get steps", "GET", "?expand=steps", "", func(c *api.Client) error {
			_, err := c.Flows.Get(context.Background(), "proj", "wf_1", api.FlowExpandSteps)
			return err
		}},
		{"update", "PATCH", "", `{"revision":3,"metadata":null,"deleted_step_ids":["old"]}`, func(c *api.Client) error {
			_, err := c.Flows.Update(context.Background(), "proj", "wf_1", api.UpdateFlow{FlowSettings: api.FlowSettings{Metadata: json.RawMessage("null")}, Revision: 3, DeletedStepIDs: []string{"old"}})
			return err
		}},
		{"duplicate", "POST", "/actions/duplicate", `{"source_version":"published"}`, func(c *api.Client) error {
			_, err := c.Flows.Duplicate(context.Background(), "proj", "wf_1", api.DuplicateFlow{SourceVersion: &published})
			return err
		}},
		{"validate latest", "POST", "/actions/validate", `{}`, func(c *api.Client) error {
			_, err := c.Flows.Validate(context.Background(), "proj", "wf_1", api.ValidateFlow{})
			return err
		}},
		{"validate revision", "POST", "/actions/validate", `{"revision":3}`, func(c *api.Client) error {
			revision := int64(3)
			_, err := c.Flows.Validate(context.Background(), "proj", "wf_1", api.ValidateFlow{Revision: &revision})
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tc.method || r.URL.RequestURI() != "/projects/proj/flows/wf_1"+tc.suffix {
					t.Errorf("request = %s %s", r.Method, r.URL.RequestURI())
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
