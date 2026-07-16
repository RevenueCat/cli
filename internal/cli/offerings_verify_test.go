package cli_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOfferingsVerifyReturnsConfigurationGraphAndIssues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/projects/proj/offerings/ofrng":
			_, _ = io.WriteString(w, `{"id":"ofrng","lookup_key":"default","display_name":"Default","is_current":true,"state":"active","created_at":1,"object":"offering"}`)
		case "/projects/proj/offerings/ofrng/packages":
			_, _ = io.WriteString(w, `{"object":"list","items":[{"id":"pkg","lookup_key":"$rc_monthly","display_name":"Monthly","created_at":1,"object":"package"}]}`)
		case "/projects/proj/packages/pkg/products":
			_, _ = io.WriteString(w, `{"object":"list","url":"/products","next_page":null,"items":[{"product":{"id":"prod","app_id":"app","created_at":1,"display_name":"Monthly","object":"product","state":"active","store_identifier":"monthly","type":"subscription"},"eligibility_criteria":"all"}]}`)
		case "/projects/proj/products/prod/prices":
			_, _ = io.WriteString(w, `[{"id":"price","currency":"USD","amount_micros":4990000}]`)
		case "/projects/proj/paywalls":
			_, _ = io.WriteString(w, `{"object":"list","items":[{"id":"pw","name":"Default","offering_id":"ofrng","created_at":1,"published_at":null,"object":"paywall"}]}`)
		case "/projects/proj/entitlements":
			_, _ = io.WriteString(w, `{"object":"list","items":[{"id":"ent","lookup_key":"premium","display_name":"Premium","created_at":1,"object":"entitlement"}]}`)
		case "/projects/proj/entitlements/ent/products":
			_, _ = io.WriteString(w, `{"object":"list","items":[{"id":"prod","app_id":"app","created_at":1,"display_name":"Monthly","object":"product","state":"active","store_identifier":"monthly","type":"subscription"}]}`)
		default:
			http.Error(w, "unexpected request: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	out, _, err := runProjectSetupCommand(t, server.URL,
		"offerings", "verify", "ofrng", "--json", "--no-input")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"store_identifier": "monthly"`, `"amount_micros": 4990000`, `"lookup_key": "premium"`, `"paywall pw is still a draft"`} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %s:\n%s", want, out)
		}
	}
}

func TestOfferingsPreviewReturnsSDKPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/projects/proj/apps/app/public_api_keys":
			_, _ = io.WriteString(w, `{"object":"list","items":[{"id":"key","object":"public_api_key","app_id":"app","environment":"production","key":"test_public","created_at":1}]}`)
		case "/v1/subscribers/preview-user/offerings":
			if r.Header.Get("Authorization") != "Bearer test_public" {
				t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
			}
			_, _ = io.WriteString(w, `{"current_offering_id":"default","offerings":[{"identifier":"default","paywall_components":null}]}`)
		default:
			http.Error(w, "unexpected request: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	out, _, err := runProjectSetupCommand(t, server.URL,
		"offerings", "preview", "app", "--app-user-id", "preview-user", "--json", "--no-input")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"current_offering_id": "default"`) || !strings.Contains(out, `"paywall_components": null`) {
		t.Fatalf("unexpected SDK payload: %s", out)
	}
}
