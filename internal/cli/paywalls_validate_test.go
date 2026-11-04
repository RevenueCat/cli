package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

func TestBuildPaywallValidationRequestMatchesSharedFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/paywall-validation-request.fixture.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Inputs struct {
			Paywall  api.Paywall     `json:"paywall"`
			Offering api.Offering    `json:"offering"`
			Apps     []api.App       `json:"apps"`
			UIConfig json.RawMessage `json:"ui_config"`
		} `json:"inputs"`
		ExpectedRequest any `json:"expected_request"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	appTypes := map[string]string{}
	for _, app := range fixture.Inputs.Apps {
		appTypes[app.ID] = string(app.Type)
	}
	version := currentComponentsVersion(fixture.Inputs.Paywall.Components)
	if version == nil {
		t.Fatal("fixture paywall has no components")
	}

	req := buildPaywallValidationRequest(&fixture.Inputs.Paywall, version, &fixture.Inputs.Offering, appTypes, fixture.Inputs.UIConfig)

	encoded, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var got any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, fixture.ExpectedRequest) {
		want, _ := json.MarshalIndent(fixture.ExpectedRequest, "", "  ")
		pretty, _ := json.MarshalIndent(got, "", "  ")
		t.Fatalf("request differs from the shared fixture\n--- want\n%s\n--- got\n%s", want, pretty)
	}
}

type publishServer struct {
	t *testing.T

	paywallStatus  int
	paywallBody    string
	offeringBody   string
	uiConfigStatus int
	validateStatus int
	validateBody   string

	validateRequest map[string]any
	published       bool
}

const (
	testPaywallBody  = `{"id":"pw_test","name":"Paywall","offering_id":"ofrng_test","created_at":1,"published_at":null,"components":{"draft":{"revision":1,"default_locale":"en_US","components_config":{"base":{}},"components_localizations":{"en_US":{}}},"published":null}}`
	testOfferingBody = `{"object":"offering","id":"ofrng_test","lookup_key":"default","display_name":"Default","is_current":true,"created_at":1,"project_id":"proj","packages":{"object":"list","url":"/p","next_page":null,"items":[{"object":"package","id":"pkg_m","lookup_key":"$rc_monthly","display_name":"Monthly","created_at":1,"products":{"object":"list","url":"/p","next_page":null,"items":[{"eligibility_criteria":"all","product":{"object":"product","id":"prod_ios","app_id":"app_ios","store_identifier":"m","type":"subscription","created_at":1}}]}}]}}`
	noIssues         = `{"valid":true,"issues":[]}`
)

func newPublishServer(t *testing.T) *publishServer {
	return &publishServer{
		t:              t,
		paywallStatus:  http.StatusOK,
		paywallBody:    testPaywallBody,
		offeringBody:   testOfferingBody,
		uiConfigStatus: http.StatusOK,
		validateStatus: http.StatusOK,
		validateBody:   noIssues,
	}
}

func (s *publishServer) start() string {
	server := httptest.NewServer(http.HandlerFunc(s.serve))
	s.t.Cleanup(server.Close)
	return server.URL
}

func (s *publishServer) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	path := r.URL.Path
	switch {
	case r.Method == http.MethodPost && path == "/projects/proj/paywalls/actions/validate":
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			s.t.Error(err)
		}
		s.validateRequest = body
		writeTestResponse(w, s.validateStatus, s.validateBody)
	case r.Method == http.MethodPost && path == "/projects/proj/paywalls/pw_test/actions/publish":
		s.published = true
		io.WriteString(w, `{"id":"pw_test","name":"Paywall","offering_id":"ofrng_test","created_at":1,"published_at":2,"object":"paywall"}`)
	case r.Method == http.MethodGet && path == "/projects/proj/paywalls/pw_test":
		writeTestResponse(w, s.paywallStatus, s.paywallBody)
	case r.Method == http.MethodGet && path == "/projects/proj/offerings/ofrng_test":
		io.WriteString(w, s.offeringBody)
	case r.Method == http.MethodGet && path == "/projects/proj/apps":
		io.WriteString(w, `{"object":"list","url":"/apps","next_page":null,"items":[{"object":"app","id":"app_ios","name":"iOS","type":"app_store","created_at":1,"project_id":"proj"}]}`)
	case r.Method == http.MethodGet && path == "/projects/proj/ui_config":
		writeTestResponse(w, s.uiConfigStatus, `{"object":"ui_config","fonts":{},"colors":{}}`)
	default:
		s.t.Errorf("unexpected request %s %s", r.Method, path)
		writeTestResponse(w, http.StatusNotFound, "")
	}
}

func writeTestResponse(w http.ResponseWriter, status int, body string) {
	if status != http.StatusOK {
		errType := map[int]string{
			http.StatusUnauthorized:    "authentication_error",
			http.StatusForbidden:       "authorization_error",
			http.StatusNotFound:        "resource_missing",
			http.StatusTooManyRequests: "rate_limit_error",
		}[status]
		if errType == "" {
			errType = "server_error"
		}
		if status == http.StatusTooManyRequests {
			w.Header().Set("Retry-After", "7")
			w.Header().Set("X-Request-Id", "req_429")
		}
		w.WriteHeader(status)
		body = `{"object":"error","type":"` + errType + `","message":"failed"}`
	}
	io.WriteString(w, body)
}

func runPublish(t *testing.T, baseURL string, extra ...string) (string, string, error) {
	t.Helper()
	return runStoreLifecycleCommand(t, baseURL, "", append([]string{"paywalls", "publish", "pw_test", "--no-input"}, extra...)...)
}

func jsonErrorEnvelope(t *testing.T, err error) errorEnvelope {
	t.Helper()
	var buf bytes.Buffer
	writeJSONError(&buf, err)
	var decoded struct {
		Error errorEnvelope `json:"error"`
	}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded.Error
}

func TestPaywallsPublishBlocksOnValidationErrors(t *testing.T) {
	s := newPublishServer(t)
	s.validateBody = `{"valid":false,"issues":[{"key":"Paywall_NoPurchaseFlow","severity":"error","message":"A paywall needs a purchase button.","component_id":"root"},{"key":"Text_Long","severity":"warning","message":"Long text."}]}`

	_, _, err := runPublish(t, s.start(), "--yes", "--json")

	var validationErr *paywallValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("error = %v, want a paywall validation error", err)
	}
	if len(validationErr.issues) != 1 || validationErr.issues[0].Key != "Paywall_NoPurchaseFlow" {
		t.Fatalf("issues = %+v, want only the error", validationErr.issues)
	}
	if !strings.Contains(err.Error(), "component root: A paywall needs a purchase button. [Paywall_NoPurchaseFlow]") {
		t.Fatalf("error message does not list the issue: %v", err)
	}
	if ExitCodeFor(err) != 1 {
		t.Fatalf("exit code = %d, want 1", ExitCodeFor(err))
	}
	if s.published {
		t.Fatal("publish was called despite validation errors")
	}

	if env := jsonErrorEnvelope(t, err); env.Type != "paywall_validation_error" || len(env.Issues) != 1 || env.Issues[0].ComponentID != "root" {
		t.Fatalf("envelope = %+v, want structured issues", env)
	}
}

func TestPaywallsPublishValidatesBeforeConfirmation(t *testing.T) {
	s := newPublishServer(t)
	s.validateBody = `{"valid":false,"issues":[{"key":"Paywall_NoPurchaseFlow","severity":"error","message":"needs a purchase button"}]}`

	_, _, err := runPublish(t, s.start())

	var validationErr *paywallValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("error = %v, want the validation error before the --yes prompt", err)
	}
}

func TestPaywallsPublishReturnsWarnings(t *testing.T) {
	s := newPublishServer(t)
	s.validateBody = `{"valid":true,"issues":[{"key":"Text_Long","severity":"warning","message":"Long text.","component_id":"title","locale":"es_ES"}]}`

	out, _, err := runPublish(t, s.start(), "--yes", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !s.published {
		t.Fatal("warnings must not block the publish")
	}
	var decoded struct {
		Data struct {
			ID          string   `json:"id"`
			PublishedAt int64    `json:"published_at"`
			Warnings    []string `json:"warnings"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	want := []string{"component title, locale es_ES: Long text. [Text_Long]"}
	if decoded.Data.ID != "pw_test" || decoded.Data.PublishedAt != 2 || !reflect.DeepEqual(decoded.Data.Warnings, want) {
		t.Fatalf("data = %+v, want the paywall plus %v", decoded.Data, want)
	}
}

func TestPaywallsPublishWithoutIssuesKeepsPaywallOutput(t *testing.T) {
	s := newPublishServer(t)

	out, _, err := runPublish(t, s.start(), "--yes", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if s.validateRequest == nil || !s.published {
		t.Fatalf("validated=%v published=%v, want both", s.validateRequest != nil, s.published)
	}
	if strings.Contains(out, "warnings") {
		t.Fatalf("output has a warnings field without warnings: %s", out)
	}
}

func TestPaywallsPublishBlocksWhenValidationIsUnavailable(t *testing.T) {
	for name, configure := range map[string]func(*publishServer){
		"validate fails":      func(s *publishServer) { s.validateStatus = http.StatusInternalServerError },
		"paywall fetch fails": func(s *publishServer) { s.paywallStatus = http.StatusInternalServerError },
		"invalid without errors": func(s *publishServer) {
			s.validateBody = `{"valid":false,"issues":[{"key":"Text_Long","severity":"warning","message":"Long text."}]}`
		},
		"empty body": func(s *publishServer) { s.validateBody = `{}` },
	} {
		t.Run(name, func(t *testing.T) {
			s := newPublishServer(t)
			configure(s)

			_, _, err := runPublish(t, s.start(), "--yes", "--json")

			var unavailable *paywallValidationUnavailableError
			if !errors.As(err, &unavailable) {
				t.Fatalf("error = %v, want validation unavailable", err)
			}
			if s.published {
				t.Fatal("publish was called without validation")
			}
		})
	}
}

func TestPaywallsPublishKeepsRateLimitDetailsWhenValidationIsUnavailable(t *testing.T) {
	s := newPublishServer(t)
	s.validateStatus = http.StatusTooManyRequests

	_, _, err := runPublish(t, s.start(), "--yes", "--json")

	if ExitCodeFor(err) != 6 {
		t.Fatalf("exit code = %d (%v), want 6", ExitCodeFor(err), err)
	}
	if env := jsonErrorEnvelope(t, err); env.Type != "paywall_validation_unavailable" || env.RetryAfterSeconds != 7 || env.RequestID != "req_429" {
		t.Fatalf("envelope = %+v, want the validation type with the rate limit details", env)
	}
	if s.published {
		t.Fatal("publish was called without validation")
	}
}

func TestDescribeValidationIssueIncludesSchemaPath(t *testing.T) {
	got := describeValidationIssue(api.PaywallValidationIssue{Key: "Schema_Invalid", Message: "Paywall schema is invalid.", Path: "components_config.base"})
	if want := "path components_config.base: Paywall schema is invalid. [Schema_Invalid]"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestPaywallsPublishPassesAuthErrorsThrough(t *testing.T) {
	for name, configure := range map[string]func(*publishServer){
		"validate 401": func(s *publishServer) { s.validateStatus = http.StatusUnauthorized },
		"validate 403": func(s *publishServer) { s.validateStatus = http.StatusForbidden },
		"paywall 403":  func(s *publishServer) { s.paywallStatus = http.StatusForbidden },
	} {
		t.Run(name, func(t *testing.T) {
			s := newPublishServer(t)
			configure(s)

			_, _, err := runPublish(t, s.start(), "--yes", "--json")

			var unavailable *paywallValidationUnavailableError
			if errors.As(err, &unavailable) {
				t.Fatalf("error = %v, want the API auth error, not validation unavailable", err)
			}
			if ExitCodeFor(err) != 4 {
				t.Fatalf("exit code = %d (%v), want 4", ExitCodeFor(err), err)
			}
			if s.published {
				t.Fatal("publish was called after an auth error")
			}
		})
	}
}

func TestPaywallsPublishSkipsValidation(t *testing.T) {
	for name, configure := range map[string]func(*publishServer){
		"missing paywall": func(s *publishServer) { s.paywallStatus = http.StatusNotFound },
		"without components": func(s *publishServer) {
			s.paywallBody = `{"id":"pw_test","offering_id":"ofrng_test","created_at":1,"published_at":null,"components":{"draft":null,"published":null}}`
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := newPublishServer(t)
			configure(s)

			if _, _, err := runPublish(t, s.start(), "--yes", "--json"); err != nil {
				t.Fatal(err)
			}
			if s.validateRequest != nil || !s.published {
				t.Fatalf("validated=%v published=%v, want publish only", s.validateRequest != nil, s.published)
			}
		})
	}
}

func TestPaywallsPublishValidatesWithBestEffortContext(t *testing.T) {
	t.Run("full context", func(t *testing.T) {
		s := newPublishServer(t)
		if _, _, err := runPublish(t, s.start(), "--yes", "--json"); err != nil {
			t.Fatal(err)
		}
		ctx := s.validateRequest["context"].(map[string]any)
		offering, _ := json.Marshal(ctx["offering"])
		if !strings.Contains(string(offering), `"products":[{"app":{"type":"app_store"},"id":"prod_ios"}]`) {
			t.Fatalf("offering = %s, want products with their app type", offering)
		}
		if ctx["ui_config"] == nil {
			t.Fatal("ui_config missing from the validation context")
		}
	})

	for name, status := range map[string]int{"ui_config forbidden": http.StatusForbidden, "ui_config fails": http.StatusInternalServerError} {
		t.Run(name, func(t *testing.T) {
			s := newPublishServer(t)
			s.uiConfigStatus = status
			if _, _, err := runPublish(t, s.start(), "--yes", "--json"); err != nil {
				t.Fatal(err)
			}
			if !s.published {
				t.Fatal("a missing ui_config must not block the publish")
			}
			if ctx := s.validateRequest["context"].(map[string]any); ctx["ui_config"] != nil || ctx["offering"] == nil {
				t.Fatalf("context = %v, want ui_config null and the offering kept", ctx)
			}
		})
	}

	t.Run("paginated offering", func(t *testing.T) {
		s := newPublishServer(t)
		s.offeringBody = strings.Replace(testOfferingBody, `"url":"/p","next_page":null,"items":[{"object":"package"`, `"url":"/p","next_page":"/p?starting_after=pkg_m","items":[{"object":"package"`, 1)
		if _, _, err := runPublish(t, s.start(), "--yes", "--json"); err != nil {
			t.Fatal(err)
		}
		if ctx := s.validateRequest["context"].(map[string]any); ctx["offering"] != nil {
			t.Fatalf("offering = %v, want null for a partial package list", ctx["offering"])
		}
	})
}
