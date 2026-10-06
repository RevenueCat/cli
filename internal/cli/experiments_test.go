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

func TestExperimentResultsNormalizesFilters(t *testing.T) {
	for _, tc := range []struct {
		platform string
		want     string
	}{
		{"ios", "iOS"},
		{"app_store", "iOS"},
		{"iOS", "iOS"},
		{"android", "Android"},
	} {
		t.Run(tc.platform, func(t *testing.T) {
			var platform, exposure string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				platform = r.URL.Query().Get("platform")
				exposure = r.URL.Query().Get("exposure_status")
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"object":"experiment_results","currency":"USD","sections":[],"statistics":[],"predicted_ltv":null}`)
			}))
			t.Cleanup(srv.Close)
			t.Setenv("RC_BASE_URL", srv.URL)
			_, _, err := runAgentCmd(t, "experiments", "results", "exp1", "--platform", tc.platform, "--exposure-status", "EXPOSED", "--project-id", "proj", "--api-key", "sk_test", "--json", "--no-input")
			if err != nil || platform != tc.want || exposure != "exposed" {
				t.Fatalf("err=%v platform=%q exposure=%q", err, platform, exposure)
			}
		})
	}
}

func TestExperimentResultsRejectsUnknownExposureStatus(t *testing.T) {
	_, _, err := runCmd(t, "experiments", "results", "exp1", "--exposure-status", "viewed")
	if err == nil || !strings.Contains(err.Error(), "exposure status must be") {
		t.Fatalf("expected supported-value error, got %v", err)
	}
}

func TestExperimentResultsWarnsOnUnknownPlatform(t *testing.T) {
	var platform string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		platform = r.URL.Query().Get("platform")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"experiment_results","currency":"USD","sections":[],"statistics":[],"predicted_ltv":null}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("RC_BASE_URL", srv.URL)
	_, stderr, err := runAgentCmd(t, "experiments", "results", "exp1", "--platform", "bogus", "--project-id", "proj", "--api-key", "sk_test", "--no-input")
	if err != nil || platform != "bogus" || !strings.Contains(stderr, `Unknown platform "bogus"`) {
		t.Fatalf("err=%v platform=%q stderr=%q", err, platform, stderr)
	}
}

func TestExperimentShowSortsTimeline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"exp1","display_name":"Timeline test","status":"paused","started_at":1000,"paused_at":3000,"resumed_at":2000}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("RC_BASE_URL", srv.URL)
	out, _, err := runAgentCmd(t, "experiments", "show", "exp1", "--project-id", "proj", "--api-key", "sk_test", "--no-input")
	started, resumed, paused := strings.Index(out, "Started:"), strings.Index(out, "Resumed:"), strings.Index(out, "Paused:")
	if err != nil || started < 0 || resumed < 0 || paused < 0 || started > resumed || resumed > paused {
		t.Fatalf("timeline out of order: err=%v out=%s", err, out)
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

func TestExperimentResultsPreservesPercentageUnitsAndShowsFilters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"currency":"USD","sections":[],"statistics":[{"metric_name":"trial_conversion_rate","variants":[{"name":"Treatment","chance_to_win":0.98,"lift_credible_interval_lower":-5,"lift_credible_interval_upper":22}]}]}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("RC_BASE_URL", srv.URL)
	out, stderr, err := runAgentCmd(t, "experiments", "results", "exp1", "--platform", "ios", "--country", "us", "--exposure-status", "exposed", "--project-id", "proj", "--api-key", "sk_test", "--no-input")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"98.0%", "-5.0% to +22.0%", "iOS", "US", "exposed"} {
		if !strings.Contains(out+stderr, want) {
			t.Fatalf("missing %q: stdout=%s stderr=%s", want, out, stderr)
		}
	}
}

func TestExperimentsListIncludesFourVariantsAndPagination(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("limit") != "5" || q.Get("starting_after") != "exp0" || q.Get("status") != "running" {
			t.Errorf("unexpected query: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"items":[{"id":"exp1","display_name":"Four variants","status":"running","offering_a":{"id":"ofrng_a"},"offering_b":{"id":"ofrng_b"},"offering_c":{"id":"ofrng_c"},"offering_d":{"id":"ofrng_d"}}],"next_page":"/projects/proj/experiments?starting_after=exp1"}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("RC_BASE_URL", srv.URL)
	args := []string{"experiments", "list", "--status", "running", "--limit", "5", "--cursor", "exp0", "--project-id", "proj", "--api-key", "sk_test", "--no-input"}
	out, stderr, err := runAgentCmd(t, args...)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ofrng_a", "ofrng_b", "ofrng_c", "ofrng_d", "--cursor exp1"} {
		if !strings.Contains(out+stderr, want) {
			t.Fatalf("missing %q: stdout=%s stderr=%s", want, out, stderr)
		}
	}
	out, stderr, err = runAgentCmd(t, append(args, "--json")...)
	if err != nil || stderr != "" || !strings.Contains(out, `"next_page"`) || !strings.Contains(out, "ofrng_d") {
		t.Fatalf("pagination or variants missing in JSON: err=%v stdout=%s stderr=%s", err, out, stderr)
	}
}

func TestExperimentShowIncludesPaywallsAndTargeting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("expand") != "offering.paywall" {
			t.Errorf("missing paywall expansion: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"exp1","display_name":"Four variants","status":"draft","offering_a":{"id":"ofrng_a","paywall_id":"pw_a"},"offering_b":{"id":"ofrng_b"},"offering_c":{"id":"ofrng_c","paywall_id":"pw_c"},"offering_d":{"id":"ofrng_d"},"targeting_conditions":[{"field":"platform","operator":"in","value":["ios"]},{"field":"country","operator":"in","value":["US"]}]}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("RC_BASE_URL", srv.URL)
	for _, jsonOutput := range []bool{false, true} {
		args := []string{"experiments", "show", "exp1", "--project-id", "proj", "--api-key", "sk_test", "--no-input"}
		if jsonOutput {
			args = append(args, "--json")
		}
		out, _, err := runAgentCmd(t, args...)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"pw_a", "pw_c", "ofrng_c", "ofrng_d", "platform", "ios", "country", "US"} {
			if !strings.Contains(out, want) {
				t.Fatalf("missing %q (json=%v): %s", want, jsonOutput, out)
			}
		}
	}
}

func TestExperimentResultsIncludesFourVariants(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"currency":"USD","sections":[{"section":"Conversion","metrics":[{"name":"trial_conversion_rate","unit":"%"}],"segments":[{"is_total":true}],"values":[{"metric":0,"segment":0,"variant":"Control","value":10},{"metric":0,"segment":0,"variant":"Treatment","value":11,"change":10},{"metric":0,"segment":0,"variant":"TreatmentC","value":12,"change":20},{"metric":0,"segment":0,"variant":"TreatmentD","value":13,"change":30}]}],"statistics":[]}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("RC_BASE_URL", srv.URL)
	out, _, err := runAgentCmd(t, "experiments", "results", "exp1", "--project-id", "proj", "--api-key", "sk_test", "--no-input")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"TREATMENT C", "TREATMENT D", "+10.00%", "+20.00%", "+30.00%"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q: %s", want, out)
		}
	}
}
