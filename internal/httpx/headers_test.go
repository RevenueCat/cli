package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/revenuecat/cli/internal/httpx"
)

func TestParseHeaders(t *testing.T) {
	got := httpx.ParseHeaders("X-RC-Route: canary-1\n  X-Trace : abc \n\nnot-a-header\nX-Empty:")
	if v := got.Get("X-Rc-Route"); v != "canary-1" {
		t.Errorf("X-RC-Route = %q, want canary-1", v)
	}
	if v := got.Get("X-Trace"); v != "abc" {
		t.Errorf("X-Trace = %q, want abc (trimmed)", v)
	}
	if _, ok := got["Not-A-Header"]; ok {
		t.Error("line without a colon must be skipped")
	}
	if v, ok := got["X-Empty"]; !ok || v[0] != "" {
		t.Errorf("empty value should still set the header, got %v", got["X-Empty"])
	}
}

func TestParseHeadersEmptyIsNil(t *testing.T) {
	for _, in := range []string{"", "   ", "\n\n", "no colons here"} {
		if h := httpx.ParseHeaders(in); h != nil {
			t.Errorf("ParseHeaders(%q) = %v, want nil", in, h)
		}
	}
}

func TestApplyOverridesAndSendsOnRequest(t *testing.T) {
	var gotRoute, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRoute = r.Header.Get("X-RC-Route")
		gotAuth = r.Header.Get("Authorization")
	}))
	t.Cleanup(srv.Close)

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Authorization", "Bearer default")
	httpx.Apply(req, httpx.ParseHeaders("X-RC-Route: canary-1\nAuthorization: Bearer override"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if gotRoute != "canary-1" {
		t.Errorf("X-RC-Route = %q, want canary-1", gotRoute)
	}
	if gotAuth != "Bearer override" {
		t.Errorf("Authorization = %q, want the applied header to override the default", gotAuth)
	}
}

func TestApplyNilIsNoop(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	req.Header.Set("Authorization", "Bearer keep")
	httpx.Apply(req, nil)
	if req.Header.Get("Authorization") != "Bearer keep" {
		t.Error("Apply(nil) must not disturb existing headers")
	}
}
