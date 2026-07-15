package appleconnect

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

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
