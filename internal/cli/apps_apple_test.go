package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/appleconnect"
	"github.com/revenuecat/cli/internal/config"
	"github.com/revenuecat/cli/internal/output"
)

func TestAppsAppleCheck_AuthenticatesAndNeverMutates(t *testing.T) {
	var mu sync.Mutex
	var revenueCatRequests []string
	revenueCat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		revenueCatRequests = append(revenueCatRequests, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/projects/proj/apps/app" {
			_, _ = io.WriteString(w, `{"id":"app","name":"iOS","type":"app_store","created_at":1,"app_store":{"bundle_id":"com.example.app","subscription_key_configured":false,"app_store_connect_api_key_configured":false}}`)
			return
		}
		http.Error(w, "unexpected request", http.StatusMethodNotAllowed)
	}))
	t.Cleanup(revenueCat.Close)

	apple := &fakeAppleCheckClient{}
	var stdout, stderr bytes.Buffer
	rt := &Runtime{
		Globals: &Globals{JSON: true, NoInput: true, Version: "test"},
		Config:  &config.Config{APIKey: "sk_test", ProjectID: "proj", BaseURL: revenueCat.URL},
		Ctx:     context.Background(),
		Out:     output.NewRenderer(&stdout, &stderr, true, true, false, ""),
		client:  api.NewClient(api.Options{APIKey: "sk_test", BaseURL: revenueCat.URL}),
	}
	cmd := newAppsAppleCmdWithFactory(func() (appleConnectClient, error) { return apple, nil })
	cmd.SetContext(WithRuntime(context.Background(), rt))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{
		"check", "app",
		"--apple-id", "dev@example.com",
		"--apple-password", "secret",
		"--verification-code", "123456",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\nstderr: %s", err, stderr.String())
	}

	mu.Lock()
	requests := append([]string(nil), revenueCatRequests...)
	mu.Unlock()
	// Two read-only fetches: the app record plus the extras (vendor number).
	for _, request := range requests {
		if request != "GET /projects/proj/apps/app" {
			t.Fatalf("RevenueCat requests = %v, want only read-only app requests", requests)
		}
	}
	if len(requests) == 0 {
		t.Fatal("no RevenueCat requests made")
	}
	if apple.mutated {
		t.Fatal("Apple check called a mutating key-creation method")
	}
	wantCalls := []string{"login", "prepare_2fa", "complete_2fa", "check_in_app_purchase", "check_app_store_connect"}
	if !equalStrings(apple.calls, wantCalls) {
		t.Fatalf("Apple calls = %v, want %v", apple.calls, wantCalls)
	}

	var output struct {
		Data struct {
			Mode                     string `json:"mode"`
			InAppPurchaseKeyAccess   bool   `json:"in_app_purchase_key_access"`
			AppStoreConnectKeyAccess bool   `json:"app_store_connect_key_access"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("decode output: %v\n%s", err, stdout.String())
	}
	if output.Data.Mode != "check" || !output.Data.InAppPurchaseKeyAccess || !output.Data.AppStoreConnectKeyAccess {
		t.Fatalf("unexpected check output: %+v", output.Data)
	}
}

type fakeAppleCheckClient struct {
	calls   []string
	mutated bool
}

func (f *fakeAppleCheckClient) Login(context.Context, string, string) (*appleconnect.Session, error) {
	f.calls = append(f.calls, "login")
	return &appleconnect.Session{}, &appleconnect.TwoFactorRequiredError{}
}

func (f *fakeAppleCheckClient) PrepareTwoFactor(context.Context, *appleconnect.Session, bool, string) (*appleconnect.Challenge, error) {
	f.calls = append(f.calls, "prepare_2fa")
	return &appleconnect.Challenge{Method: "trusted_device", CodeLength: 6}, nil
}

func (f *fakeAppleCheckClient) CompleteTwoFactor(_ context.Context, session *appleconnect.Session, _ string) error {
	f.calls = append(f.calls, "complete_2fa")
	session.Provider = appleconnect.Provider{ID: 7, PublicID: "issuer", Name: "Example Team"}
	session.Providers = []appleconnect.Provider{session.Provider}
	return nil
}

func (f *fakeAppleCheckClient) SelectProvider(context.Context, *appleconnect.Session, int64) error {
	f.calls = append(f.calls, "select_provider")
	return nil
}

func (f *fakeAppleCheckClient) CheckKeyAccess(_ context.Context, _ *appleconnect.Session, kind appleconnect.KeyKind) error {
	f.calls = append(f.calls, "check_"+string(kind))
	return nil
}

func (f *fakeAppleCheckClient) CreateInAppPurchaseKey(context.Context, *appleconnect.Session, string) (*appleconnect.Key, error) {
	f.mutated = true
	return nil, errors.New("unexpected In-App Purchase key creation")
}

func (f *fakeAppleCheckClient) CreateAppStoreConnectKey(context.Context, *appleconnect.Session, string) (*appleconnect.Key, error) {
	f.mutated = true
	return nil, errors.New("unexpected App Store Connect key creation")
}

func (f *fakeAppleCheckClient) FetchVendorNumber(context.Context, *appleconnect.Session) (string, error) {
	return "", errors.New("vendor number unavailable in tests")
}

func (f *fakeAppleCheckClient) AppExists(context.Context, *appleconnect.Session, string) (bool, error) {
	return true, nil
}

func (f *fakeAppleCheckClient) RegisterBundleID(context.Context, *appleconnect.Session, string, string) error {
	f.mutated = true
	return errors.New("unexpected bundle ID registration")
}

func (f *fakeAppleCheckClient) CreateApp(context.Context, *appleconnect.Session, string, string, string) error {
	f.mutated = true
	return errors.New("unexpected App Store Connect app creation")
}

type fakeAppleSetupClient struct {
	fakeAppleCheckClient
	inApp          func() (*appleconnect.Key, error)
	api            func() (*appleconnect.Key, error)
	registerErr    error
	createErr      error
	registerCalled bool
	createCalled   bool
}

func (f *fakeAppleSetupClient) CreateInAppPurchaseKey(context.Context, *appleconnect.Session, string) (*appleconnect.Key, error) {
	return f.inApp()
}

func (f *fakeAppleSetupClient) CreateAppStoreConnectKey(context.Context, *appleconnect.Session, string) (*appleconnect.Key, error) {
	return f.api()
}

func (f *fakeAppleSetupClient) RegisterBundleID(context.Context, *appleconnect.Session, string, string) error {
	f.registerCalled = true
	return f.registerErr
}

func (f *fakeAppleSetupClient) CreateApp(context.Context, *appleconnect.Session, string, string, string) error {
	f.createCalled = true
	return f.createErr
}

func TestCreateAppleKeys_NonFatalOnOneFailure(t *testing.T) {
	inAppKey := &appleconnect.Key{Kind: appleconnect.InAppPurchaseKey, ID: "iap1", IssuerID: "iss", PrivateKey: "iap-pem"}
	apiKey := &appleconnect.Key{Kind: appleconnect.AppStoreConnectKey, ID: "asc1", IssuerID: "iss", PrivateKey: "asc-pem"}
	maxKeys := errors.New("You've reached the maximum number of active keys allowed. To generate a new key, revoke an existing one.")
	forbidden := errors.New("403 FORBIDDEN_ERROR: The API key in use does not allow this request")

	tests := []struct {
		name       string
		inApp      func() (*appleconnect.Key, error)
		api        func() (*appleconnect.Key, error)
		wantInApp  *appleconnect.Key
		wantAPI    *appleconnect.Key
		wantFailed []string
		wantHint   string
	}{
		{
			name:       "in-app fails, ASC succeeds",
			inApp:      func() (*appleconnect.Key, error) { return nil, forbidden },
			api:        func() (*appleconnect.Key, error) { return apiKey, nil },
			wantAPI:    apiKey,
			wantFailed: []string{"in-app purchase key"},
			wantHint:   "already exists",
		},
		{
			name:       "ASC fails, in-app succeeds",
			inApp:      func() (*appleconnect.Key, error) { return inAppKey, nil },
			api:        func() (*appleconnect.Key, error) { return nil, maxKeys },
			wantInApp:  inAppKey,
			wantFailed: []string{"App Store Connect API key"},
			wantHint:   "limit of active keys",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apple := &fakeAppleSetupClient{inApp: tt.inApp, api: tt.api}
			var stdout, stderr bytes.Buffer
			rt := &Runtime{
				Globals: &Globals{NoInput: true, Version: "test"},
				Out:     output.NewRenderer(&stdout, &stderr, false, false, false, ""),
			}
			keys := createAppleKeys(context.Background(), rt, apple, &appleconnect.Session{}, true, true, "in-app", "api")

			if keys.InAppPurchaseKey != tt.wantInApp {
				t.Errorf("InAppPurchaseKey = %v, want %v", keys.InAppPurchaseKey, tt.wantInApp)
			}
			if keys.AppStoreConnectKey != tt.wantAPI {
				t.Errorf("AppStoreConnectKey = %v, want %v", keys.AppStoreConnectKey, tt.wantAPI)
			}
			if !equalStrings(keys.Failed, tt.wantFailed) {
				t.Errorf("Failed = %v, want %v", keys.Failed, tt.wantFailed)
			}
			if !strings.Contains(stderr.String(), tt.wantHint) {
				t.Errorf("stderr missing hint %q:\n%s", tt.wantHint, stderr.String())
			}
		})
	}
}

func TestAppsAppleSetup_PartialFailureExitsNonZeroAndReportsFailedKey(t *testing.T) {
	inAppKey := &appleconnect.Key{Kind: appleconnect.InAppPurchaseKey, ID: "iap1", IssuerID: "iss", PrivateKey: "iap-pem"}
	maxKeys := errors.New("You've reached the maximum number of active keys allowed.")

	var patched bool
	revenueCat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/projects/proj/apps/app" {
			if r.Method == http.MethodPost {
				patched = true
			}
			_, _ = io.WriteString(w, `{"id":"app","name":"iOS","type":"app_store","created_at":1,"app_store":{"bundle_id":"com.example.app","subscription_key_configured":false,"app_store_connect_api_key_configured":false}}`)
			return
		}
		http.Error(w, "unexpected request", http.StatusMethodNotAllowed)
	}))
	t.Cleanup(revenueCat.Close)

	apple := &fakeAppleSetupClient{
		inApp: func() (*appleconnect.Key, error) { return inAppKey, nil },
		api:   func() (*appleconnect.Key, error) { return nil, maxKeys },
	}
	var stdout, stderr bytes.Buffer
	rt := &Runtime{
		Globals: &Globals{JSON: true, NoInput: true, AssumeYes: true, Version: "test"},
		Config:  &config.Config{APIKey: "sk_test", ProjectID: "proj", BaseURL: revenueCat.URL},
		Ctx:     context.Background(),
		Out:     output.NewRenderer(&stdout, &stderr, true, true, false, ""),
		client:  api.NewClient(api.Options{APIKey: "sk_test", BaseURL: revenueCat.URL}),
	}
	cmd := newAppsAppleCmdWithFactory(func() (appleConnectClient, error) { return apple, nil })
	// Without this, cobra appends usage text to stdout on the non-zero return.
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetContext(WithRuntime(context.Background(), rt))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{
		"setup", "app",
		"--apple-id", "dev@example.com",
		"--apple-password", "secret",
		"--verification-code", "123456",
	})

	err := cmd.Execute()
	var exit *SilentExitError
	if !errors.As(err, &exit) || exit.Code == 0 {
		t.Fatalf("execute err = %v, want SilentExitError with non-zero code\nstderr: %s", err, stderr.String())
	}
	if !patched {
		t.Error("expected the in-app key that succeeded to be saved to RevenueCat (no PATCH seen)")
	}

	var out struct {
		Data struct {
			InAppPurchaseKeyID string   `json:"in_app_purchase_key_id"`
			Failed             []string `json:"failed_keys"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode output: %v\n%s", err, stdout.String())
	}
	if out.Data.InAppPurchaseKeyID != "iap1" {
		t.Errorf("in_app_purchase_key_id = %q, want the saved key iap1", out.Data.InAppPurchaseKeyID)
	}
	if !equalStrings(out.Data.Failed, []string{ascKeyLabel}) {
		t.Errorf("failed_keys = %v, want [%q]", out.Data.Failed, ascKeyLabel)
	}
}

// A --force replace that fails leaves the previously-configured key in place, so
// the card must say "kept existing", not "not created".
func TestAppsAppleSetup_FailedReplaceReadsAsKeptExisting(t *testing.T) {
	forbidden := errors.New("403 FORBIDDEN_ERROR: The API key in use does not allow this request")
	apiKey := &appleconnect.Key{Kind: appleconnect.AppStoreConnectKey, ID: "asc1", IssuerID: "iss", PrivateKey: "asc-pem"}

	revenueCat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/projects/proj/apps/app" {
			_, _ = io.WriteString(w, `{"id":"app","name":"iOS","type":"app_store","created_at":1,"app_store":{"bundle_id":"com.example.app","subscription_key_configured":true,"app_store_connect_api_key_configured":false}}`)
			return
		}
		http.Error(w, "unexpected request", http.StatusMethodNotAllowed)
	}))
	t.Cleanup(revenueCat.Close)

	// In-app key already configured; --force tries to replace it and Apple refuses.
	apple := &fakeAppleSetupClient{
		inApp: func() (*appleconnect.Key, error) { return nil, forbidden },
		api:   func() (*appleconnect.Key, error) { return apiKey, nil },
	}
	var stdout, stderr bytes.Buffer
	rt := &Runtime{
		Globals: &Globals{NoInput: true, AssumeYes: true, Version: "test"},
		Config:  &config.Config{APIKey: "sk_test", ProjectID: "proj", BaseURL: revenueCat.URL},
		Ctx:     context.Background(),
		Out:     output.NewRenderer(&stdout, &stderr, false, true, false, ""),
		client:  api.NewClient(api.Options{APIKey: "sk_test", BaseURL: revenueCat.URL}),
	}
	cmd := newAppsAppleCmdWithFactory(func() (appleConnectClient, error) { return apple, nil })
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetContext(WithRuntime(context.Background(), rt))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{
		"setup", "app", "--force",
		"--apple-id", "dev@example.com", "--apple-password", "secret", "--verification-code", "123456",
	})
	_ = cmd.Execute() // exits non-zero for the failed key; we only assert the label here

	if !strings.Contains(stdout.String(), "kept existing (replace failed") {
		t.Fatalf("a failed replace of a configured key should read as kept existing, got:\n%s", stdout.String())
	}
}

func TestCreateAppStoreAppRecord_NonFatalOnAppleFailure(t *testing.T) {
	cases := []struct {
		name        string
		registerErr error
		createErr   error
		wantCreate  bool
		wantWarn    string
	}{
		{
			name:        "register bundle id fails",
			registerErr: errors.New(`An App ID with Identifier 'com.example.app' is not available.`),
			wantCreate:  false,
			wantWarn:    "is not available",
		},
		{
			name:       "create app fails",
			createErr:  errors.New("boom"),
			wantCreate: true,
			wantWarn:   "Could not create the App Store Connect app record: boom",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			apple := &fakeAppleSetupClient{registerErr: tc.registerErr, createErr: tc.createErr}
			var stdout, stderr bytes.Buffer
			rt := &Runtime{
				Globals: &Globals{},
				Out:     output.NewRenderer(&stdout, &stderr, false, true, false, ""),
			}
			if err := createAppStoreAppRecord(context.Background(), rt, apple, &appleconnect.Session{}, "com.example.app", "Example", "com.example.app"); err != nil {
				t.Fatalf("createAppStoreAppRecord returned error, want nil (non-fatal): %v", err)
			}
			if !apple.registerCalled {
				t.Fatal("RegisterBundleID was not called")
			}
			if apple.createCalled != tc.wantCreate {
				t.Fatalf("CreateApp called = %v, want %v", apple.createCalled, tc.wantCreate)
			}
			if !strings.Contains(stderr.String(), tc.wantWarn) {
				t.Fatalf("warning = %q, want it to contain %q", stderr.String(), tc.wantWarn)
			}
			if !strings.Contains(stderr.String(), "Continuing with key setup") {
				t.Fatalf("warning = %q, want it to mention continuing with key setup", stderr.String())
			}
		})
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
