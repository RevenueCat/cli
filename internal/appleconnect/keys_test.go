package appleconnect

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateKeysUsesAppleIrisShapesAndDownloadsOnce(t *testing.T) {
	pem := "-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----\n"
	seen := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("X-CSRF-ITC") != "[asc-ui]" {
			t.Errorf("missing Apple CSRF header")
		}
		key := r.Method + " " + r.URL.Path
		seen[key] = true
		switch key {
		case "POST /iris/v1/apiKeys":
			assertKeyCreateBody(t, r, "apiKeys", "RevenueCat", true)
			_, _ = io.WriteString(w, `{"data":{"id":"ASC123","attributes":{"canDownload":true}}}`)
		case "GET /iris/v1/apiKeys/ASC123":
			if r.URL.Query().Get("fields[apiKeys]") != "privateKey" {
				t.Errorf("unexpected query: %s", r.URL.RawQuery)
			}
			writePrivateKey(t, w, pem)
		case "POST /iris/v1/subscriptionKeys":
			assertKeyCreateBody(t, r, "subscriptionKeys", "RevenueCat", false)
			_, _ = io.WriteString(w, `{"data":{"id":"IAP123","attributes":{"canDownload":true}}}`)
		case "GET /iris/v1/subscriptionKeys/IAP123":
			if r.URL.Query().Get("fields[subscriptionKeys]") != "privateKey" {
				t.Errorf("unexpected query: %s", r.URL.RawQuery)
			}
			writePrivateKey(t, w, pem)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client, err := New(Options{HTTPClient: server.Client(), ASCBaseURL: server.URL, AuthBaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	session := &Session{client: client, Provider: Provider{PublicID: "issuer-id"}}
	ascKey, err := client.CreateAppStoreConnectKey(context.Background(), session, "RevenueCat")
	if err != nil {
		t.Fatal(err)
	}
	iapKey, err := client.CreateInAppPurchaseKey(context.Background(), session, "RevenueCat")
	if err != nil {
		t.Fatal(err)
	}
	if ascKey.ID != "ASC123" || iapKey.ID != "IAP123" || ascKey.IssuerID != "issuer-id" || ascKey.PrivateKey != pem {
		t.Fatalf("unexpected keys: asc=%+v iap=%+v", ascKey, iapKey)
	}
	for _, request := range []string{"POST /iris/v1/apiKeys", "GET /iris/v1/apiKeys/ASC123", "POST /iris/v1/subscriptionKeys", "GET /iris/v1/subscriptionKeys/IAP123"} {
		if !seen[request] {
			t.Errorf("missing %s", request)
		}
	}
}

func TestCheckKeyAccessUsesReadOnlyListEndpoints(t *testing.T) {
	seen := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		seen[key] = true
		if r.Method != http.MethodGet || r.URL.Query().Get("limit") != "1" {
			t.Errorf("unexpected access check: %s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	t.Cleanup(server.Close)

	client, err := New(Options{HTTPClient: server.Client(), ASCBaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	session := &Session{client: client, Provider: Provider{PublicID: "issuer-id"}}
	for _, kind := range []KeyKind{InAppPurchaseKey, AppStoreConnectKey} {
		if err := client.CheckKeyAccess(context.Background(), session, kind); err != nil {
			t.Fatal(err)
		}
	}
	for _, request := range []string{"GET /iris/v1/apiKeys", "GET /iris/v1/subscriptionKeys"} {
		if !seen[request] {
			t.Errorf("missing %s", request)
		}
	}
}

func assertKeyCreateBody(t *testing.T, r *http.Request, resourceType, name string, expectRole bool) {
	t.Helper()
	var body struct {
		Data struct {
			Type       string         `json:"type"`
			Attributes map[string]any `json:"attributes"`
		} `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Data.Type != resourceType || body.Data.Attributes["nickname"] != name {
		t.Fatalf("unexpected create body: %+v", body)
	}
	_, hasRoles := body.Data.Attributes["roles"]
	if hasRoles != expectRole {
		t.Fatalf("roles present = %v, want %v", hasRoles, expectRole)
	}
}

func writePrivateKey(t *testing.T, w http.ResponseWriter, pem string) {
	t.Helper()
	payload := map[string]any{"data": map[string]any{"attributes": map[string]string{"privateKey": base64.StdEncoding.EncodeToString([]byte(pem))}}}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatal(err)
	}
}
