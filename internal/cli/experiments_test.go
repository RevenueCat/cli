package cli_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExperimentResultsJSONIncludesAllSegments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects/proj/experiments/exp1/results" {
			t.Errorf("unexpected route: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"experiment_results","currency":"USD","sections":[{"object":"experiment_results_section","section":"Revenue","metrics":[{"name":"realized_ltv_per_customer"}],"segments":[{"object":"experiment_results_segment","id":"product","display_name":"Annual","is_total":false}],"values":[{"object":"experiment_results_value","metric":0,"segment":0,"variant":"Treatment","value":12.5,"change":5,"credible_interval":null,"lift_credible_interval":null}]}],"statistics":[],"predicted_ltv":null}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("RC_BASE_URL", srv.URL)
	out, stderr, err := runAgentCmd(t, "experiments", "results", "exp1", "--project-id", "proj", "--api-key", "sk_test", "--json", "--no-input")
	if err != nil || stderr != "" {
		t.Fatalf("err=%v stderr=%q", err, stderr)
	}
	var envelope struct {
		Data struct {
			Sections []struct {
				Segments []struct {
					ID string `json:"id"`
				} `json:"segments"`
			} `json:"sections"`
		} `json:"data"`
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.SchemaVersion != 1 || envelope.Data.Sections[0].Segments[0].ID != "product" {
		t.Fatalf("unexpected envelope: %s", out)
	}
}

func TestExperimentResultsHumanExplainsMissingTotals(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"experiment_results","currency":"USD","sections":[{"section":"Revenue","metrics":[{"name":"revenue"}],"segments":[{"id":"product","display_name":"Annual","is_total":false}],"values":[{"metric":0,"segment":0,"variant":"Treatment","value":12.5}]}],"statistics":[],"predicted_ltv":{"predicted_winner_variant":"b","confidence":85}}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("RC_BASE_URL", srv.URL)
	out, stderr, err := runAgentCmd(t, "experiments", "results", "exp1", "--project-id", "proj", "--api-key", "sk_test", "--no-input")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"No total-segment metrics available", "Use --json", "Predicted winner", "Treatment"} {
		if !strings.Contains(out+stderr, want) {
			t.Fatalf("missing %q in output: stdout=%s stderr=%s", want, out, stderr)
		}
	}
}

func TestExperimentsDiscoverable(t *testing.T) {
	out, _, err := runCmd(t, "schema", "experiments", "results", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"path": "rc experiments results"`) || strings.Contains(out, `"experimental": true`) {
		t.Fatalf("results command not discoverable as a default command: %s", out)
	}
}
