package cli_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProductsListDurationRendering(t *testing.T) {
	const body = `{"object":"list","items":[{"object":"product","id":"prod_sub","display_name":"Premium Monthly","type":"subscription","store_identifier":"rc_monthly","state":"active","app_id":"app_x","created_at":1658399423658,"subscription":{"duration":"P1M","grace_period_duration":"P3D","trial_duration":"P1W"}}],"next_page":null,"url":"/products"}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/products") {
			io.WriteString(w, body)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"object":"error","type":"resource_missing","message":"not found"}`)
	}))
	t.Cleanup(server.Close)
	t.Setenv("RC_BASE_URL", server.URL)

	base := []string{"products", "list", "--no-input", "--project-id", "proj_x", "--api-key", "sk_x"}

	humanOut, _, err := runAgentCmd(t, base...)
	if err != nil {
		t.Fatalf("human run failed: %v", err)
	}
	if !strings.Contains(humanOut, "1 month") {
		t.Errorf("human output missing friendly duration %q; got:\n%s", "1 month", humanOut)
	}
	if strings.Contains(humanOut, "P1M") {
		t.Errorf("human output leaked raw ISO duration P1M; got:\n%s", humanOut)
	}

	jsonOut, _, err := runAgentCmd(t, append(base, "--json")...)
	if err != nil {
		t.Fatalf("json run failed: %v", err)
	}
	if !strings.Contains(jsonOut, `"duration": "P1M"`) {
		t.Errorf("json output lost raw duration P1M; got:\n%s", jsonOut)
	}
	if strings.Contains(jsonOut, "1 month") {
		t.Errorf("json output contained humanized duration; got:\n%s", jsonOut)
	}
}
