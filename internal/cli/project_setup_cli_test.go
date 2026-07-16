package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/revenuecat/cli/internal/cli"
)

func TestOfferingsSetCurrent_RequiresConfirmationAndUpdatesOffering(t *testing.T) {
	var isCurrent *bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/projects/proj/offerings/ofrng" {
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		var body struct {
			IsCurrent *bool `json:"is_current"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		isCurrent = body.IsCurrent
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"ofrng","lookup_key":"default","display_name":"Default","is_current":true,"state":"active","object":"offering","created_at":1,"metadata":null}`)
	}))
	t.Cleanup(server.Close)

	out, _, err := runProjectSetupCommand(t, server.URL,
		"offerings", "set-current", "ofrng", "--yes", "--json", "--no-input")
	if err != nil {
		t.Fatal(err)
	}
	if isCurrent == nil || !*isCurrent || !strings.Contains(out, `"is_current": true`) {
		t.Fatalf("is_current = %v, output = %s", isCurrent, out)
	}
}

func TestOfferingsSetCurrent_NoInputRequiresYes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("request made without confirmation")
	}))
	t.Cleanup(server.Close)

	_, _, err := runProjectSetupCommand(t, server.URL,
		"offerings", "set-current", "ofrng", "--json", "--no-input")
	if err == nil || !strings.Contains(err.Error(), "pass --yes") {
		t.Fatalf("error = %v, want --yes guidance", err)
	}
}

func TestAppsKeys_ReturnsTypedPublicSDKKeys(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/projects/proj/apps/app/public_api_keys" {
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","items":[{"id":"key","object":"public_api_key","app_id":"app","environment":"production","key":"appl_test","created_at":1}],"next_page":null,"url":"/keys"}`)
	}))
	t.Cleanup(server.Close)

	out, _, err := runProjectSetupCommand(t, server.URL,
		"apps", "keys", "app", "--json", "--no-input")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"key": "appl_test"`) || !strings.Contains(out, `"environment": "production"`) || !strings.Contains(out, `"key_type": "App Store"`) {
		t.Fatalf("unexpected SDK key output: %s", out)
	}
}

func TestProductsCreate_SendsTestStoreTitleAndDuration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/projects/proj/products" {
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		var body struct {
			AppID        string `json:"app_id"`
			Title        string `json:"title"`
			Subscription struct {
				Duration string `json:"duration"`
			} `json:"subscription"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.AppID != "app_test" || body.Title != "Premium Monthly" || body.Subscription.Duration != "P1M" {
			t.Fatalf("unexpected product body: %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"prod_test","store_identifier":"premium_monthly","type":"subscription","app_id":"app_test","display_name":"Premium Monthly","created_at":1,"object":"product"}`)
	}))
	t.Cleanup(server.Close)

	out, _, err := runProjectSetupCommand(t, server.URL,
		"products", "create",
		"--app-id", "app_test",
		"--store-id", "premium_monthly",
		"--type", "subscription",
		"--display-name", "Premium Monthly",
		"--title", "Premium Monthly",
		"--duration", "P1M",
		"--json", "--no-input")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"id": "prod_test"`) {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestProductsPricesSet_CreatesMissingAndUpdatesExistingCurrencies(t *testing.T) {
	getCalls := 0
	var updatedMicros int64
	var created []struct {
		Currency     string `json:"currency"`
		AmountMicros int64  `json:"amount_micros"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/proj/products/prod_test/prices":
			getCalls++
			if getCalls == 1 {
				_, _ = io.WriteString(w, `[{"id":"price_usd","currency":"USD","amount_micros":5000000}]`)
				return
			}
			_, _ = io.WriteString(w, `[{"id":"price_usd","currency":"USD","amount_micros":9990000},{"id":"price_eur","currency":"EUR","amount_micros":8990000}]`)
		case r.Method == http.MethodPatch && r.URL.Path == "/projects/proj/products/prod_test/prices/USD":
			var body struct {
				AmountMicros int64 `json:"amount_micros"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			updatedMicros = body.AmountMicros
			_, _ = io.WriteString(w, `{"id":"price_usd","currency":"USD","amount_micros":9990000}`)
		case r.Method == http.MethodPost && r.URL.Path == "/projects/proj/products/prod_test/test_store_prices":
			var body struct {
				Prices []struct {
					Currency     string `json:"currency"`
					AmountMicros int64  `json:"amount_micros"`
				} `json:"prices"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			created = body.Prices
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `[{"id":"price_eur","currency":"EUR","amount_micros":8990000}]`)
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	out, _, err := runProjectSetupCommand(t, server.URL,
		"products", "prices", "set", "prod_test",
		"--price", "USD=9.99",
		"--price", "EUR=8.99",
		"--json", "--no-input")
	if err != nil {
		t.Fatal(err)
	}
	if updatedMicros != 9_990_000 {
		t.Fatalf("updated amount = %d", updatedMicros)
	}
	if len(created) != 1 || created[0].Currency != "EUR" || created[0].AmountMicros != 8_990_000 {
		t.Fatalf("created prices = %+v", created)
	}
	if !strings.Contains(out, `"currency": "USD"`) || !strings.Contains(out, `"currency": "EUR"`) {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestPaywallsCreate_CreatesDraftForOffering(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/projects/proj/paywalls" {
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		var body struct {
			OfferingID                 string `json:"offering_id"`
			AutomaticallyScaleFontSize bool   `json:"automatically_scale_font_size"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.OfferingID != "ofrng_default" || !body.AutomaticallyScaleFontSize {
			t.Fatalf("unexpected paywall body: %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"pw_test","name":"Default Paywall","offering_id":"ofrng_default","created_at":1,"published_at":null,"automatically_scale_font_size":true,"object":"paywall"}`)
	}))
	t.Cleanup(server.Close)

	out, _, err := runProjectSetupCommand(t, server.URL,
		"paywalls", "create", "--offering-id", "ofrng_default", "--json", "--no-input")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"id": "pw_test"`) || !strings.Contains(out, `"offering_id": "ofrng_default"`) || !strings.Contains(out, `"published_at": null`) {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestPaywallsPublishRequiresConfirmationAndReturnsState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/projects/proj/paywalls/pw_test/actions/publish" {
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"pw_test","name":"Default Paywall","offering_id":"ofrng_default","created_at":1,"published_at":2,"object":"paywall"}`)
	}))
	t.Cleanup(server.Close)

	_, _, err := runProjectSetupCommand(t, server.URL,
		"paywalls", "publish", "pw_test", "--json", "--no-input")
	if err == nil || !strings.Contains(err.Error(), "pass --yes") {
		t.Fatalf("error = %v, want --yes guidance", err)
	}

	out, _, err := runProjectSetupCommand(t, server.URL,
		"paywalls", "publish", "pw_test", "--yes", "--json", "--no-input")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"published_at": 2`) {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestPaywallsUnpublishRequiresConfirmationAndReturnsState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/projects/proj/paywalls/pw_test/actions/unpublish" {
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"pw_test","name":"Default Paywall","offering_id":"ofrng_default","created_at":1,"published_at":null,"object":"paywall"}`)
	}))
	t.Cleanup(server.Close)

	_, _, err := runProjectSetupCommand(t, server.URL,
		"paywalls", "unpublish", "pw_test", "--json", "--no-input")
	if err == nil || !strings.Contains(err.Error(), "pass --yes") {
		t.Fatalf("error = %v, want --yes guidance", err)
	}

	out, _, err := runProjectSetupCommand(t, server.URL,
		"paywalls", "unpublish", "pw_test", "--yes", "--json", "--no-input")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"published_at": null`) {
		t.Fatalf("unexpected output: %s", out)
	}
}

func runProjectSetupCommand(t *testing.T, baseURL string, args ...string) (string, string, error) {
	t.Helper()
	t.Setenv("RC_CONFIG_DIR", t.TempDir())
	t.Setenv("RC_BASE_URL", baseURL)
	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd("test")
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	args = append(args, "--api-key", "sk_test", "--project-id", "proj")
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return stdout.String(), stderr.String(), err
}
