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
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("X-Requested-With") != "XMLHttpRequest" {
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
			if r.Header.Get("Authorization") != "Bearer "+loginToken {
				t.Errorf("unexpected authorization header: %q", r.Header.Get("Authorization"))
			}
			redirect, _ := url.Parse(r.URL.Query().Get("redirect_uri"))
			query := redirect.Query()
			query.Set("code", "authorization-code")
			query.Set("state", r.URL.Query().Get("state"))
			redirect.RawQuery = query.Encode()
			_, _ = fmt.Fprintf(w, `{"redirect_uri":%q}`, redirect.String())
		case "/v1/developers/logout":
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	service := NewOAuthService(server.URL, "cli-client")
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

	want := []string{
		"POST /v1/developers/provision-account",
		"POST /v1/developers/login",
		"POST /v1/developers/me/oauth-authorize",
		"POST /v1/developers/logout",
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

	service := NewOAuthService(server.URL, "cli-client")
	_, err := service.AuthorizeWithLoginToken(context.Background(), "token", "http://localhost:49152/callback", "challenge", "state")
	if err == nil {
		t.Fatal("expected mismatched redirect error")
	}
}
