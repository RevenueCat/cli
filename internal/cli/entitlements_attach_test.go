package cli

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// entitlements attach/detach accept a lookup key ("pro") and resolve it to
// the entitlement ID before calling the API, since the command examples have
// always shown lookup keys.
func TestEntitlementsAttach_ResolvesLookupKeyToID(t *testing.T) {
	var attachedID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/entitlements"):
			io.WriteString(w, `{"object":"list","items":[{"object":"entitlement","id":"entl_abc","lookup_key":"pro","display_name":"Pro"}],"next_page":null,"url":"/entitlements"}`)
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/entitlements/"):
			attachedID = strings.Split(strings.TrimPrefix(r.URL.Path, "/projects/proj/entitlements/"), "/")[0]
			io.WriteString(w, `{"object":"list","items":[]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"object":"error","type":"resource_missing","message":"not found"}`)
		}
	}))
	defer server.Close()

	t.Setenv("RC_CONFIG_DIR", t.TempDir())
	t.Setenv("RC_BASE_URL", server.URL)
	root := NewRootCmd("test")
	root.SetArgs([]string{"entitlements", "attach", "pro", "prod_monthly", "--json", "--no-input", "--api-key", "sk_test", "--project-id", "proj"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("attach by lookup key failed: %v", err)
	}
	if attachedID != "entl_abc" {
		t.Fatalf("lookup key 'pro' should resolve to entl_abc, attached to %q", attachedID)
	}
}
