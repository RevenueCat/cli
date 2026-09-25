package cli_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestExperimentStartRequiresApprovalBeforeMutation(t *testing.T) {
	mutations := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			mutations++
			_, _ = io.WriteString(w, `{"object":"experiment","id":"exp1","display_name":"New paywall","status":"running","created_at":1,"updated_at":2}`)
			return
		}
		_, _ = io.WriteString(w, `{"object":"experiment","id":"exp1","display_name":"New paywall","status":"draft","created_at":1,"updated_at":2,"enrollment_percentage":50,"offering_a":{"id":"ofrng_a"},"offering_b":{"id":"ofrng_b"}}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("RC_BASE_URL", srv.URL)
	args := []string{"experiments", "start", "exp1", "--project-id", "proj", "--api-key", "sk_test", "--no-input"}
	_, _, err := runAgentCmd(t, args...)
	if err == nil || !strings.Contains(err.Error(), "--yes") || mutations != 0 {
		t.Fatalf("expected approval error before mutation; err=%v mutations=%d", err, mutations)
	}
	args = append(args, "--yes", "--json")
	out, stderr, err := runAgentCmd(t, args...)
	if err != nil || stderr != "" || mutations != 1 || !strings.Contains(out, `"status": "running"`) {
		t.Fatalf("approved start failed: err=%v stderr=%q mutations=%d out=%s", err, stderr, mutations, out)
	}
}

func TestExperimentResumeRequiresApprovalBeforeMutation(t *testing.T) {
	mutations := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			mutations++
			_, _ = io.WriteString(w, `{"id":"exp1","display_name":"New paywall","status":"running"}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"exp1","display_name":"New paywall","status":"paused","enrollment_percentage":50,"offering_a":{"id":"ofrng_a"},"offering_b":{"id":"ofrng_b"}}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("RC_BASE_URL", srv.URL)
	args := []string{"experiments", "resume", "exp1", "--project-id", "proj", "--api-key", "sk_test", "--no-input"}
	_, stderr, err := runAgentCmd(t, args...)
	if err == nil || !strings.Contains(err.Error(), "--yes") || mutations != 0 || !strings.Contains(stderr, "Control") {
		t.Fatalf("expected reviewed approval before resume; err=%v mutations=%d stderr=%q", err, mutations, stderr)
	}
	args = append(args, "--yes", "--json")
	_, _, err = runAgentCmd(t, args...)
	if err != nil || mutations != 1 {
		t.Fatalf("approved resume failed: err=%v mutations=%d", err, mutations)
	}
}

func TestRunningExperimentUpdateShowsChangesBeforeApproval(t *testing.T) {
	mutations := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			mutations++
		}
		_, _ = io.WriteString(w, `{"id":"exp1","display_name":"New paywall","status":"running","enrollment_percentage":50,"offering_a":{"id":"ofrng_a"},"offering_b":{"id":"ofrng_b"}}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("RC_BASE_URL", srv.URL)
	configPath := filepath.Join(t.TempDir(), "changes.json")
	if err := os.WriteFile(configPath, []byte(`{"enrollment_percentage":80}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := runAgentCmd(t, "experiments", "update", "exp1", "--config", configPath, "--project-id", "proj", "--api-key", "sk_test", "--no-input")
	if err == nil || !strings.Contains(err.Error(), "--yes") || mutations != 0 || !strings.Contains(stderr, `"enrollment_percentage":80`) {
		t.Fatalf("expected reviewed approval before update; err=%v mutations=%d stderr=%q", err, mutations, stderr)
	}
}

func TestExperimentCreateListsMissingFlags(t *testing.T) {
	_, _, err := runCmd(t, "experiments", "create", "--project-id", "proj", "--api-key", "sk_test", "--no-input")
	if err == nil {
		t.Fatal("expected missing input error")
	}
	for _, flag := range []string{"--name", "--control", "--treatment", "--enrollment"} {
		if !strings.Contains(err.Error(), flag) {
			t.Fatalf("missing %s from error: %v", flag, err)
		}
	}
}
