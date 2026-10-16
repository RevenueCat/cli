package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPaywallsDeleteRequiresForceForAttachedOrPublished(t *testing.T) {
	paywalls := map[string]string{
		"pw_attached":   `{"object":"paywall","id":"pw_attached","name":"Hero","offering_id":"ofrng_x","created_at":1700000000000,"published_at":null}`,
		"pw_published":  `{"object":"paywall","id":"pw_published","name":"Live","offering_id":"ofrng_live","created_at":1700000000000,"published_at":1700000000000}`,
		"pw_standalone": `{"object":"paywall","id":"pw_standalone","created_at":1700000000000,"published_at":null}`,
	}
	var deletedPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		switch r.Method {
		case http.MethodGet:
			io.WriteString(w, paywalls[id])
		case http.MethodDelete:
			deletedPaths = append(deletedPaths, r.URL.Path)
			io.WriteString(w, `{}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	t.Setenv("RC_CONFIG_DIR", t.TempDir())
	t.Setenv("RC_BASE_URL", server.URL)

	del := func(args ...string) error {
		root := NewRootCmd("test")
		root.SetOut(io.Discard)
		root.SetErr(&bytes.Buffer{})
		root.SetArgs(append([]string{"paywalls", "delete"}, append(args, "--yes", "--api-key", "sk_test", "--project-id", "proj")...))
		return root.ExecuteContext(context.Background())
	}

	for _, id := range []string{"pw_attached", "pw_published"} {
		if err := del(id); err == nil {
			t.Fatalf("%s should require --force", id)
		}
	}
	if len(deletedPaths) != 0 {
		t.Fatalf("refusals must not issue DELETE, got %v", deletedPaths)
	}

	if err := del("pw_standalone"); err != nil {
		t.Fatalf("standalone draft should delete without --force: %v", err)
	}
	if err := del("pw_attached", "--force"); err != nil {
		t.Fatalf("--force should delete attached paywall: %v", err)
	}
	want := []string{"/projects/proj/paywalls/pw_standalone", "/projects/proj/paywalls/pw_attached"}
	if len(deletedPaths) != 2 || deletedPaths[0] != want[0] || deletedPaths[1] != want[1] {
		t.Fatalf("DELETE paths = %v, want %v", deletedPaths, want)
	}
}
