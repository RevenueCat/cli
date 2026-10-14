package cli_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFlowsList_ShowsTypeAndPublishState(t *testing.T) {
	var gotExclude []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects/proj/flows" {
			http.Error(w, "unexpected request: "+r.URL.Path, http.StatusNotFound)
			return
		}
		gotExclude = r.URL.Query()["exclude_type"]
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","next_page":"/v2/projects/proj/flows?starting_after=wf_2","items":[
			{"id":"wf_1","object":"flow","name":"Onboarding","type":"paywall","created_at":1767225600000,"published_at":null,"has_unpublished_changes":true},
			{"id":"wf_2","object":"flow","name":"Checkout","type":"web-funnel","created_at":1767225600000,"published_at":1767225600000,"has_unpublished_changes":true}
		]}`)
	}))
	t.Cleanup(server.Close)

	out, stderr, err := runProjectSetupCommand(t, server.URL, "flows", "list", "--exclude-type", "general", "--no-input")
	if err != nil {
		t.Fatal(err)
	}
	if len(gotExclude) != 1 || gotExclude[0] != "general" {
		t.Errorf("exclude_type = %v, want [general]", gotExclude)
	}
	for _, want := range []string{"Onboarding", "never", "web-funnel", "(draft changes)"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(stderr, "--cursor wf_2") {
		t.Errorf("expected next-page hint on stderr, got:\n%s", stderr)
	}
}

func TestFlowsList_RejectsTypeWithExcludeType(t *testing.T) {
	_, _, err := runProjectSetupCommand(t, "http://127.0.0.1:0", "flows", "list", "--type", "paywall", "--exclude-type", "general", "--no-input")
	if err == nil {
		t.Fatal("expected an error combining --type and --exclude-type")
	}
}
