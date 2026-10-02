package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestOAuthSignupEndpoints(t *testing.T) {
	const loginToken = "temporary-token"
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if got := r.Header.Get("User-Agent"); got != "rc-test/1" {
			t.Errorf("%s User-Agent = %q", r.URL.Path, got)
		}
		if got := r.Header.Get("X-Trace"); got != "test-trace" {
			t.Errorf("%s X-Trace = %q", r.URL.Path, got)
		}
		if r.URL.Path == "/v1/developers/provision-account" || r.URL.Path == "/v1/developers/login" || r.URL.Path == "/oauth2/token" {
			if got := r.Header.Get("Authorization"); got != "" {
				t.Errorf("%s unexpected Authorization = %q", r.URL.Path, got)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/oauth2/token" && r.Header.Get("X-Requested-With") != "XMLHttpRequest" {
			t.Errorf("%s missing required X-Requested-With header", r.URL.Path)
		}
		switch r.URL.Path {
		case "/v1/developers/provision-account":
			var account ProvisionAccountRequest
			if err := json.NewDecoder(r.Body).Decode(&account); err != nil {
				t.Error(err)
			}
			if account.Email != "dev@example.com" || account.Password != "generated" || !account.MarketingEmailEnabled {
				t.Errorf("unexpected account: %+v", account)
			}
			w.WriteHeader(http.StatusCreated)
		case "/v1/developers/login":
			_, _ = fmt.Fprintf(w, `{"authentication_token":%q}`, loginToken)
		case "/v1/developers/me/oauth-authorize":
			if got := r.URL.Query().Get("scope"); got != DefaultOAuthScope {
				t.Errorf("scope = %q, want %q", got, DefaultOAuthScope)
			}
			if r.Header.Get("Authorization") != "Bearer "+loginToken {
				t.Errorf("unexpected authorization header: %q", r.Header.Get("Authorization"))
			}
			redirect, _ := url.Parse(r.URL.Query().Get("redirect_uri"))
			query := redirect.Query()
			query.Set("code", "authorization-code")
			query.Set("state", r.URL.Query().Get("state"))
			redirect.RawQuery = query.Encode()
			_, _ = fmt.Fprintf(w, `{"redirect_uri":%q}`, redirect.String())
		case "/oauth2/token":
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			if r.Form.Get("client_id") != "cli-client" {
				t.Errorf("unexpected client_id: %q", r.Form.Get("client_id"))
			}
			_, _ = fmt.Fprint(w, `{"access_token":"access","refresh_token":"refresh","expires_in":3600}`)
		case "/v1/developers/logout":
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	service := NewOAuthService(OAuthOptions{
		BaseURL:      server.URL,
		ClientID:     "cli-client",
		UserAgent:    "rc-test/1",
		ExtraHeaders: http.Header{"X-Trace": {"test-trace"}, "Authorization": {"Bearer override"}},
	})
	ctx := context.Background()
	if err := service.ProvisionAccount(ctx, ProvisionAccountRequest{
		Email: "dev@example.com", Name: "Developer", Password: "generated", MarketingEmailEnabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	login, err := service.Login(ctx, "dev@example.com", "generated")
	if err != nil {
		t.Fatal(err)
	}
	code, err := service.AuthorizeWithLoginToken(ctx, login.AuthenticationToken, "http://localhost:49152/callback", "challenge", "state")
	if err != nil {
		t.Fatal(err)
	}
	if code != "authorization-code" {
		t.Fatalf("code = %q", code)
	}
	if err := service.LogoutLoginToken(ctx, login.AuthenticationToken); err != nil {
		t.Fatal(err)
	}

	if _, err := service.ExchangeCode(ctx, code, "http://localhost:49152/callback", "verifier"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Refresh(ctx, "refresh-token"); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"POST /v1/developers/provision-account",
		"POST /v1/developers/login",
		"POST /v1/developers/me/oauth-authorize",
		"POST /v1/developers/logout",
		"POST /oauth2/token",
		"POST /oauth2/token",
	}
	if fmt.Sprint(calls) != fmt.Sprint(want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestAuthorizeWithLoginTokenRejectsMismatchedRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"redirect_uri":"https://attacker.example/callback?code=stolen&state=state"}`))
	}))
	t.Cleanup(server.Close)

	service := NewOAuthService(OAuthOptions{BaseURL: server.URL, ClientID: "cli-client"})
	_, err := service.AuthorizeWithLoginToken(context.Background(), "token", "http://localhost:49152/callback", "challenge", "state")
	if err == nil {
		t.Fatal("expected mismatched redirect error")
	}
}
