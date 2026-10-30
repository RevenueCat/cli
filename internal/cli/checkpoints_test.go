package cli_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCheckpointsList_ShowsIdentifiersAndPaginates(t *testing.T) {
	var gotLimit, gotCursor string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects/proj/checkpoints" {
			http.Error(w, "unexpected request: "+r.URL.Path, http.StatusNotFound)
			return
		}
		gotLimit = r.URL.Query().Get("limit")
		gotCursor = r.URL.Query().Get("starting_after")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","next_page":"/v2/projects/proj/checkpoints?starting_after=chkpt_2","items":[
			{"id":"chkpt_1","object":"checkpoint","project_id":"proj","identifier":"app_open","created_via":"api","created_at":1767225600000},
			{"id":"chkpt_2","object":"checkpoint","project_id":"proj","identifier":"after_onboarding","created_via":"dashboard","created_at":1767225600000}
		]}`)
	}))
	t.Cleanup(server.Close)

	out, stderr, err := runProjectSetupCommand(t, server.URL, "checkpoints", "list", "--limit", "2", "--cursor", "chkpt_0", "--no-input")
	if err != nil {
		t.Fatal(err)
	}
	if gotLimit != "2" || gotCursor != "chkpt_0" {
		t.Errorf("limit=%q starting_after=%q, want 2 and chkpt_0", gotLimit, gotCursor)
	}
	for _, want := range []string{"chkpt_1", "app_open", "after_onboarding", "dashboard"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(stderr, "--cursor chkpt_2") {
		t.Errorf("expected next-page hint on stderr, got:\n%s", stderr)
	}
}

func TestCheckpointsList_JSONReturnsRawPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","next_page":null,"items":[{"id":"chkpt_1","object":"checkpoint","identifier":"app_open","created_at":1}]}`)
	}))
	t.Cleanup(server.Close)

	out, _, err := runProjectSetupCommand(t, server.URL, "checkpoints", "list", "--json", "--no-input")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"schema_version": 1`, `"identifier": "app_open"`} {
		if !strings.Contains(out, want) {
			t.Errorf("json output missing %s:\n%s", want, out)
		}
	}
}
