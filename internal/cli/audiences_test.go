package cli_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const audienceJSON = `{
	"id":"aud_1","object":"audience","project_id":"proj","customer_list_id":"cl_1","name":"Pro in Germany",
	"rules":{"groups":[
		{"conditions":[{"field":"country","operator":"is","value":"DE"},{"field":"customAttribute:plan","operator":"is","value":"pro"}]},
		{"conditions":[{"field":"totalSpent","operator":"greaterThan","value":"100","currency":"EUR"}]}
	]},
	"created_at":1767225600000
}`

func audiencesServer(t *testing.T, gotExpand *[]string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/projects/proj/audiences":
			_, _ = io.WriteString(w, `{"object":"list","next_page":null,"url":"/v2/projects/proj/audiences","items":[`+audienceJSON+`]}`)
		case "/projects/proj/audiences/aud_1":
			if gotExpand != nil {
				*gotExpand = r.URL.Query()["expand"]
			}
			body := strings.TrimSuffix(strings.TrimSpace(audienceJSON), "}") +
				`,"used_by":{"object":"audience_used_by","targeting_rules":[{"object":"targeting_rule_reference","id":"rule_1","display_name":"Germany promo","state":"active"}],"experiments":[]}}`
			_, _ = io.WriteString(w, body)
		default:
			http.Error(w, "unexpected request: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestAudiencesList_SummarisesRules(t *testing.T) {
	server := audiencesServer(t, nil)

	out, _, err := runProjectSetupCommand(t, server.URL, "audiences", "list", "--no-input")
	if err != nil {
		t.Fatal(err)
	}
	want := "(country is DE and customAttribute:plan is pro) or (totalSpent greaterThan 100 EUR)"
	if !strings.Contains(out, "aud_1") || !strings.Contains(out, want) {
		t.Errorf("list output missing audience or rule summary:\n%s", out)
	}
}

func TestAudiencesShow_UsageFlagExpandsUsedBy(t *testing.T) {
	var gotExpand []string
	server := audiencesServer(t, &gotExpand)

	out, _, err := runProjectSetupCommand(t, server.URL, "audiences", "show", "aud_1", "--usage", "--no-input")
	if err != nil {
		t.Fatal(err)
	}
	if len(gotExpand) != 1 || gotExpand[0] != "used_by" {
		t.Errorf("expand = %v, want [used_by]", gotExpand)
	}
	for _, want := range []string{"Pro in Germany", "Germany promo", "Targeting rule", "customer-lists/cl_1"} {
		if !strings.Contains(out, want) {
			t.Errorf("show output missing %q:\n%s", want, out)
		}
	}
}

func TestAudiencesShow_JSONReturnsRawAudience(t *testing.T) {
	server := audiencesServer(t, nil)

	out, _, err := runProjectSetupCommand(t, server.URL, "audiences", "show", "aud_1", "--json", "--no-input")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"schema_version": 1`, `"customer_list_id": "cl_1"`, `"operator": "greaterThan"`} {
		if !strings.Contains(out, want) {
			t.Errorf("json output missing %s:\n%s", want, out)
		}
	}
}
