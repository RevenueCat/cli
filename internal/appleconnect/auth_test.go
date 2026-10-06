package appleconnect

import (
	"context"
	"crypto/pbkdf2"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestCalculateProofMatchesFastlaneSIRP(t *testing.T) {
	n, _ := new(big.Int).SetString(appleSRPModulusHex, 16)
	g := big.NewInt(2)
	a := big.NewInt(123456)
	A := new(big.Int).Exp(g, a, n)
	prepared, err := preparePassword("secret", "s2k")
	if err != nil {
		t.Fatal(err)
	}
	salt := []byte("salt")
	derived, err := pbkdf2.Key(sha256.New, string(prepared), salt, 1, derivedPasswordLen)
	if err != nil {
		t.Fatal(err)
	}
	m1, m2, err := calculateProof("dev@example.com", a, A, n, g, []byte{2}, derived, salt)
	if err != nil {
		t.Fatal(err)
	}
	// Generated independently with pinned fastlane-sirp code to catch padding
	// and proof differences. Source and reproduction: testdata/srp-proof-fixture.md.
	if m1 != "OL3tMhvYaZcmgV8KO40CG1UZ6Rgw4dDF1bSGsEN6c4s=" || m2 != "548X9ZQ5iGyqHhSBA3s2lDdzcZThics3GqSP20346T0=" {
		t.Fatalf("proofs differ from fastlane-sirp: m1=%s m2=%s", m1, m2)
	}
}

func TestCalculateProofRejectsZeroServerPublicValue(t *testing.T) {
	n, _ := new(big.Int).SetString(appleSRPModulusHex, 16)
	for _, serverB := range [][]byte{nil, {0}, n.Bytes(), new(big.Int).Mul(n, big.NewInt(2)).Bytes()} {
		_, _, err := calculateProof("dev@example.com", big.NewInt(1), big.NewInt(2), n, big.NewInt(2), serverB, []byte("password"), []byte("salt"))
		if err == nil || !strings.Contains(err.Error(), "invalid Apple SRP server public value") {
			t.Fatalf("expected invalid server public value, got %v", err)
		}
	}
}

func TestLoginSRPRequestContract(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/logout":
			w.Header().Set("Location", "/signout?widgetKey=widget")
			w.WriteHeader(http.StatusFound)
		case "/auth/signin/init":
			if r.Header.Get("X-Apple-Widget-Key") != "widget" || r.Header.Get("Accept") != "application/json, text/javascript" || r.Header.Get("X-Requested-With") != "XMLHttpRequest" {
				t.Errorf("unexpected SRP init headers: %v", r.Header)
			}
			var payload struct {
				A           string   `json:"a"`
				AccountName string   `json:"accountName"`
				Protocols   []string `json:"protocols"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			if payload.A == "" || payload.AccountName != "dev@example.com" || strings.Join(payload.Protocols, ",") != "s2k,s2k_fo" {
				t.Errorf("unexpected SRP init payload: %+v", payload)
			}
			http.SetCookie(w, &http.Cookie{Name: "DES-session", Value: "value/with/slashes", Path: "/"})
			_, _ = io.WriteString(w, `{"iteration":1,"salt":"c2FsdA==","protocol":"s2k","b":"Ag==","c":"challenge"}`)
		case "/auth/signin":
			if r.Method != http.MethodGet || r.URL.Query().Get("widgetKey") != "widget" {
				t.Errorf("unexpected hashcash request: %s %s", r.Method, r.URL)
			}
			w.Header().Set("X-Apple-HC-Bits", "1")
			w.Header().Set("X-Apple-HC-Challenge", "challenge")
		case "/auth/signin/complete":
			if r.URL.Query().Get("isRememberMeEnabled") != "false" || r.Header.Get("X-Apple-Widget-Key") != "widget" || r.Header.Get("Accept") != "application/json, text/javascript" {
				t.Errorf("unexpected SRP completion request: %s %v", r.URL, r.Header)
			}
			if !strings.Contains(r.Header.Get("Cookie"), `DES-session="value/with/slashes"`) {
				t.Errorf("missing quoted DES cookie: %q", r.Header.Get("Cookie"))
			}
			hashcash := r.Header.Get("X-Apple-HC")
			digest := sha1.Sum([]byte(hashcash))
			if !strings.HasPrefix(hashcash, "1:1:") || !leadingZeroBits(digest[:], 1) {
				t.Errorf("invalid hashcash: %q", hashcash)
			}
			var payload struct {
				AccountName string `json:"accountName"`
				Challenge   string `json:"c"`
				M1          string `json:"m1"`
				M2          string `json:"m2"`
				RememberMe  bool   `json:"rememberMe"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			if payload.AccountName != "dev@example.com" || payload.Challenge != "challenge" || payload.RememberMe {
				t.Errorf("unexpected SRP completion payload: %+v", payload)
			}
			for _, proof := range []string{payload.M1, payload.M2} {
				decoded, err := base64.StdEncoding.DecodeString(proof)
				if err != nil || len(decoded) != 32 {
					t.Errorf("invalid proof encoding: %q", proof)
				}
			}
			_, _ = io.WriteString(w, `{}`)
		case "/olympus/v1/session":
			_, _ = io.WriteString(w, `{"provider":{"providerId":42,"publicProviderId":"issuer","name":"Example"}}`)
		default:
			t.Errorf("unexpected request: %s", r.URL)
		}
	}))
	t.Cleanup(server.Close)
	client, err := New(Options{HTTPClient: server.Client(), ASCBaseURL: server.URL, AuthBaseURL: server.URL + "/auth"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := client.Login(context.Background(), " dev@example.com ", "secret")
	if err != nil || session == nil || session.Provider.ID != 42 {
		t.Fatalf("session = %+v, error = %v", session, err)
	}
	want := "HEAD /logout\nPOST /auth/signin/init\nGET /auth/signin\nPOST /auth/signin/complete\nGET /olympus/v1/session"
	if got := strings.Join(requests, "\n"); got != want {
		t.Fatalf("requests:\n%s\nwant:\n%s", got, want)
	}
}

func TestLoginHandlesSignInResponses(t *testing.T) {
	cases := []struct {
		name        string
		status      int
		body        string
		wantError   error
		wantMessage string
		twoFactor   bool
	}{
		{name: "signed in", status: http.StatusOK, body: `{}`},
		{name: "two factor", status: http.StatusConflict, body: `{}`, twoFactor: true},
		{name: "invalid credentials", status: http.StatusUnauthorized, body: `{}`, wantError: ErrInvalidCredentials},
		{name: "forbidden", status: http.StatusForbidden, body: `{}`, wantError: ErrInvalidCredentials},
		{name: "account action", status: http.StatusPreconditionFailed, body: `{}`, wantError: ErrAccountAction},
		{name: "service error", status: http.StatusOK, body: `{"serviceErrors":[{"code":"-22421","message":"Try again later."}]}`, wantMessage: "Try again later. (-22421)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var sessionRequests int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/logout":
					w.Header().Set("Location", "/signout?widgetKey=widget")
					w.WriteHeader(http.StatusFound)
				case "/auth/signin/init":
					_, _ = io.WriteString(w, `{"iteration":1,"salt":"c2FsdA==","protocol":"s2k","b":"Ag==","c":"challenge"}`)
				case "/auth/signin":
					w.WriteHeader(http.StatusOK)
				case "/auth/signin/complete":
					w.Header().Set("X-Apple-ID-Session-Id", "session")
					w.Header().Set("scnt", "continuation")
					w.WriteHeader(tc.status)
					_, _ = io.WriteString(w, tc.body)
				case "/olympus/v1/session":
					sessionRequests++
					_, _ = io.WriteString(w, `{"provider":{"providerId":42,"publicProviderId":"issuer","name":"Example"}}`)
				default:
					t.Errorf("unexpected request: %s", r.URL)
				}
			}))
			t.Cleanup(server.Close)
			client, err := New(Options{HTTPClient: server.Client(), ASCBaseURL: server.URL, AuthBaseURL: server.URL + "/auth"})
			if err != nil {
				t.Fatal(err)
			}
			session, err := client.Login(context.Background(), "dev@example.com", "secret")
			switch {
			case tc.twoFactor:
				var twoFactor *TwoFactorRequiredError
				if !errors.As(err, &twoFactor) || session == nil || session.AppleIDSessionID != "session" || session.SCNT != "continuation" {
					t.Fatalf("missing two-factor continuation: session = %+v, error = %v", session, err)
				}
			case tc.wantMessage != "":
				if err == nil || !strings.Contains(err.Error(), tc.wantMessage) {
					t.Fatalf("error = %v, want message %q", err, tc.wantMessage)
				}
			case tc.wantError != nil:
				if !errors.Is(err, tc.wantError) {
					t.Fatalf("error = %v, want %v", err, tc.wantError)
				}
			default:
				if err != nil || session == nil || session.Provider.ID != 42 {
					t.Fatalf("session = %+v, error = %v", session, err)
				}
			}
			wantSessionRequests := 0
			if !tc.twoFactor && tc.wantError == nil && tc.wantMessage == "" {
				wantSessionRequests = 1
			}
			if sessionRequests != wantSessionRequests {
				t.Fatalf("session requests = %d, want %d", sessionRequests, wantSessionRequests)
			}
		})
	}
}

func TestAuthServiceKeyUsesIsolatedSignoutRequest(t *testing.T) {
	var logoutRequests, signoutRequests, olympusRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/logout":
			logoutRequests++
			if logoutRequests <= 2 && (r.Method != http.MethodHead || r.Header.Get("Cookie") != "") {
				t.Errorf("logout request = %s, cookies = %q", r.Method, r.Header.Get("Cookie"))
			}
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "destroyed", Path: "/"})
			w.Header().Set("Location", "/appleauth/signout?asop=destroy-session&widgetKey=fresh%2Bkey%3D")
			w.WriteHeader(http.StatusFound)
		case "/appleauth/signout":
			signoutRequests++
		case "/olympus/v1/app/config":
			olympusRequests++
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	httpClient := server.Client()
	var redirects int
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		redirects++
		return nil
	}
	client, err := New(Options{HTTPClient: httpClient, ASCBaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	baseURL, _ := url.Parse(server.URL)
	client.httpClient.Jar.SetCookies(baseURL, []*http.Cookie{{Name: "session", Value: "active", Path: "/"}})
	for range 2 {
		key, err := client.authServiceKey(context.Background())
		if err != nil || key != "fresh+key=" {
			t.Fatalf("service key = %q, error = %v", key, err)
		}
	}
	if logoutRequests != 2 || signoutRequests != 0 || olympusRequests != 0 || redirects != 0 {
		t.Fatalf("requests: logout=%d signout=%d olympus=%d redirects=%d", logoutRequests, signoutRequests, olympusRequests, redirects)
	}
	if cookies := client.httpClient.Jar.Cookies(baseURL); len(cookies) != 1 || cookies[0].Value != "active" {
		t.Fatalf("session cookies changed: %v", cookies)
	}
	resp, err := client.httpClient.Get(server.URL + "/logout")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if redirects != 1 || signoutRequests != 1 {
		t.Fatal("the original client's redirect policy was changed")
	}
}

func TestAuthServiceKeyFallsBackToOlympus(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		location string
		payload  string
	}{
		{"missing key", http.StatusFound, "/signout?other=value", `{"authServiceKey":"fallback"}`},
		{"empty key", http.StatusFound, "/signout?widgetKey=%20", `{"serviceKey":"fallback"}`},
		{"missing location", http.StatusFound, "", `{"authServiceKey":"fallback"}`},
		{"invalid location", http.StatusFound, "://invalid", `{"authServiceKey":"fallback"}`},
		{"invalid query", http.StatusFound, "/signout?widgetKey=%ZZ", `{"authServiceKey":"fallback"}`},
		{"server error", http.StatusServiceUnavailable, "/signout?widgetKey=wrong", `{"authServiceKey":"fallback"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/logout":
					w.Header().Set("Location", tc.location)
					w.WriteHeader(tc.status)
				case "/olympus/v1/app/config":
					if r.Method != http.MethodGet || r.URL.Query().Get("hostname") != "itunesconnect.apple.com" {
						t.Errorf("unexpected fallback request: %s %s", r.Method, r.URL)
					}
					_, _ = io.WriteString(w, tc.payload)
				default:
					t.Errorf("unexpected request: %s", r.URL)
				}
			}))
			t.Cleanup(server.Close)
			client, err := New(Options{HTTPClient: server.Client(), ASCBaseURL: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			key, err := client.authServiceKey(context.Background())
			if err != nil || key != "fallback" {
				t.Fatalf("service key = %q, error = %v", key, err)
			}
		})
	}
}

func TestAuthServiceKeyPreservesBothFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/logout" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	client, err := New(Options{HTTPClient: server.Client(), ASCBaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.authServiceKey(context.Background())
	if err == nil || !strings.Contains(err.Error(), "signout redirect: status 503") || !strings.Contains(err.Error(), "status 404") {
		t.Fatalf("expected both service-key failures, got %v", err)
	}
}

func TestAuthServiceKeyDoesNotFallBackAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var requests int
	client, err := New(Options{HTTPClient: &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		requests++
		cancel()
		return nil, context.Canceled
	})}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.authServiceKey(ctx)
	if !errors.Is(err, context.Canceled) || requests != 1 {
		t.Fatalf("error = %v, requests = %d", err, requests)
	}
}

func TestPrepareTwoFactorReportsAppleServiceErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"noTrustedDevices":true,"serviceErrors":[{"code":"-28248","message":"Verification codes can't be sent to this phone number at this time."}]}`)
	}))
	t.Cleanup(server.Close)
	client, err := New(Options{HTTPClient: server.Client(), AuthBaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	session := &Session{client: client, ServiceKey: "widget", AppleIDSessionID: "session", SCNT: "scnt"}
	_, err = client.PrepareTwoFactor(context.Background(), session, false, "")
	if err == nil || !strings.Contains(err.Error(), "Verification codes can't be sent") || !strings.Contains(err.Error(), "-28248") || !strings.Contains(err.Error(), "no trusted devices") {
		t.Fatalf("expected Apple's SMS delivery error, got %v", err)
	}
}

func TestCompleteTwoFactorStopsOnAppleVerificationErrors(t *testing.T) {
	for _, field := range []string{"serviceErrors", "service_errors", "validationErrors"} {
		t.Run(field, func(t *testing.T) {
			var requests []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests = append(requests, r.URL.Path)
				_, _ = io.WriteString(w, `{"`+field+`":[{"code":-21669,"title":"Incorrect Verification Code"}]}`)
			}))
			t.Cleanup(server.Close)
			client, err := New(Options{HTTPClient: server.Client(), AuthBaseURL: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			session := &Session{client: client, ServiceKey: "widget", AppleIDSessionID: "session", SCNT: "scnt", factorMethod: "trusteddevice"}
			err = client.CompleteTwoFactor(context.Background(), session, "123456")
			if err == nil || !strings.Contains(err.Error(), "Incorrect Verification Code (-21669)") {
				t.Fatalf("expected verification error, got %v", err)
			}
			if len(requests) != 1 || requests[0] != "/verify/trusteddevice/securitycode" {
				t.Fatalf("continued after rejected verification: %v", requests)
			}
		})
	}
}

func TestPreparePasswordMatchesAppleProtocols(t *testing.T) {
	s2k, err := preparePassword("secret", "s2k")
	if err != nil {
		t.Fatal(err)
	}
	s2kFO, err := preparePassword("secret", "s2k_fo")
	if err != nil {
		t.Fatal(err)
	}
	if len(s2k) != 32 {
		t.Fatalf("s2k digest length = %d, want 32", len(s2k))
	}
	if len(s2kFO) != 64 {
		t.Fatalf("s2k_fo digest length = %d, want 64", len(s2kFO))
	}
	if _, err := preparePassword("secret", "unknown"); err == nil {
		t.Fatal("expected unsupported protocol error")
	}
}

func TestPhoneMatchesFastlaneCases(t *testing.T) {
	cases := map[string]string{
		"+49 123 4567885": "+49 •••• •••••85",
		"+1-123-456-7866": "+1 (•••) •••-••66",
		"+353123456743":   "+353 •• ••• ••43",
		"+4900000000011":  "+49\u00a0•••••••••11",
	}
	for number, masked := range cases {
		if !phoneMatches(number, masked) {
			t.Errorf("expected %q to match %q", number, masked)
		}
	}
	if phoneMatches("+1-123-456-7800", "+1 (•••) •••-••66") {
		t.Fatal("unexpected phone match")
	}
}

func TestHashcashSatisfiesRequestedBits(t *testing.T) {
	value := makeHashcash(10, "challenge", time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC))
	digest := sha1.Sum([]byte(value))
	if !leadingZeroBits(digest[:], 10) {
		t.Fatalf("hashcash does not satisfy 10 bits: %q", value)
	}
}

func TestQuoteDESCookies(t *testing.T) {
	header := http.Header{"Cookie": []string{"myacinfo=value; DES5cbbc=value%2Fwith%2Fslashes; other=ok"}}
	quoteDESCookies(header)
	got := header.Get("Cookie")
	want := `myacinfo=value; DES5cbbc="value%2Fwith%2Fslashes"; other=ok`
	if got != want {
		t.Fatalf("cookie header = %q, want %q", got, want)
	}
}

func TestPrepareAndCompleteSMSFactor(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/appleauth/auth":
			_, _ = io.WriteString(w, `{"noTrustedDevices":true,"trustedPhoneNumbers":[{"id":7,"pushMode":"sms","numberWithDialCode":"+1 (•••) •••-••66"}],"securityCode":{"length":6}}`)
		case "/appleauth/auth/verify/phone/securitycode", "/appleauth/auth/2sv/trust":
			w.WriteHeader(http.StatusNoContent)
		case "/olympus/v1/session":
			_, _ = io.WriteString(w, `{"provider":{"providerId":42,"publicProviderId":"issuer-id","name":"Example"},"user":{"emailAddress":"dev@example.com"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client, err := New(Options{HTTPClient: server.Client(), AuthBaseURL: server.URL + "/appleauth/auth", ASCBaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	session := &Session{client: client, ServiceKey: "widget", AppleIDSessionID: "session", SCNT: "scnt"}
	challenge, err := client.PrepareTwoFactor(context.Background(), session, true, "+1-123-456-7866")
	if err != nil {
		t.Fatal(err)
	}
	if challenge.Method != "sms" || challenge.Destination == "" {
		t.Fatalf("unexpected challenge: %+v", challenge)
	}
	if err := client.CompleteTwoFactor(context.Background(), session, "123456"); err != nil {
		t.Fatal(err)
	}
	if session.Provider.PublicID != "issuer-id" {
		t.Fatalf("issuer = %q", session.Provider.PublicID)
	}
	joined := strings.Join(requests, "\n")
	if strings.Contains(joined, "PUT /appleauth/auth/verify/phone") {
		t.Fatalf("single fallback phone should already have received a code:\n%s", joined)
	}
	if !strings.Contains(joined, "POST /appleauth/auth/verify/phone/securitycode") {
		t.Fatalf("missing phone verification request:\n%s", joined)
	}
}

func TestSelectProviderVerifiesRefreshedSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/olympus/v1/session":
			_, _ = io.WriteString(w, `{}`)
		case r.Method == http.MethodGet && r.URL.Path == "/olympus/v1/session":
			_, _ = io.WriteString(w, `{"provider":{"providerId":7,"publicProviderId":"issuer","name":"Selected"},"availableProviders":[{"providerId":7,"publicProviderId":"issuer","name":"Selected"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client, err := New(Options{HTTPClient: server.Client(), ASCBaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	session := &Session{client: client, Provider: Provider{ID: 1}, Providers: []Provider{{ID: 7, Name: "Selected"}}}
	if err := client.SelectProvider(context.Background(), session, 7); err != nil {
		t.Fatal(err)
	}
	if session.Provider.ID != 7 || session.Provider.PublicID != "issuer" {
		t.Fatalf("unexpected selected provider: %+v", session.Provider)
	}
}

func TestDecodePrivateKeyAcceptsBase64PEM(t *testing.T) {
	pem := "-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----\n"
	got, err := decodePrivateKey(base64.StdEncoding.EncodeToString([]byte(pem)))
	if err != nil {
		t.Fatal(err)
	}
	if got != pem {
		t.Fatalf("decoded PEM = %q", got)
	}
}
