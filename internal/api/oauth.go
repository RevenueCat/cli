package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const DefaultOAuthBaseURL = "https://api.revenuecat.com"
const DefaultOAuthClientID = "cmV2ZW51ZWNhdC1jbGk="
const DefaultOAuthScope = "*:*:read_write"

// OAuthService handles the token endpoint only — the browser redirect and
// callback server live in the CLI layer since they have user-facing side effects.
type OAuthService struct {
	baseURL    string
	clientID   string
	httpClient *http.Client
}

func NewOAuthService(baseURL, clientID string) *OAuthService {
	return &OAuthService{
		baseURL:    strings.TrimRight(baseURL, "/"),
		clientID:   clientID,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// TokenResponse is the successful response from /oauth2/token.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"` // seconds
	TokenType    string `json:"token_type"`
}

type ProvisionAccountRequest struct {
	Email                 string `json:"email"`
	Name                  string `json:"name"`
	Password              string `json:"password"`
	MarketingEmailEnabled bool   `json:"marketing_email_enabled"`
}

type LoginResponse struct {
	AuthenticationToken string `json:"authentication_token"`
}

type authorizationResponse struct {
	RedirectURI string `json:"redirect_uri"`
}

// AuthorizeURL builds the /auth/authorize URL the user's browser should visit.
// state is a caller-generated random value that the server echoes back; callers
// must verify it in the callback to prevent CSRF.
func (s *OAuthService) AuthorizeURL(redirectURI, challenge, state string) string {
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {s.clientID},
		"redirect_uri":          {redirectURI},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"scope":                 {DefaultOAuthScope},
		"state":                 {state},
	}
	return s.baseURL + "/oauth2/authorize?" + q.Encode()
}

// GenerateState returns a random URL-safe string suitable for use as the OAuth
// state parameter.
func GenerateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ExchangeCode exchanges an authorization code for tokens.
func (s *OAuthService) ExchangeCode(ctx context.Context, code, redirectURI, verifier string) (*TokenResponse, error) {
	return s.postToken(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {s.clientID},
		"code_verifier": {verifier},
	})
}

func (s *OAuthService) ProvisionAccount(ctx context.Context, account ProvisionAccountRequest) error {
	return s.postJSON(ctx, "/v1/developers/provision-account", "", account, nil)
}

func (s *OAuthService) Login(ctx context.Context, email, password string) (*LoginResponse, error) {
	var response LoginResponse
	err := s.postJSON(ctx, "/v1/developers/login", "", map[string]string{
		"email": email, "password": password,
	}, &response)
	if err != nil {
		return nil, err
	}
	if response.AuthenticationToken == "" {
		return nil, fmt.Errorf("login response did not include an authentication token")
	}
	return &response, nil
}

func (s *OAuthService) AuthorizeWithLoginToken(ctx context.Context, loginToken, redirectURI, challenge, state string) (string, error) {
	query := url.Values{
		"response_type":         {"code"},
		"client_id":             {s.clientID},
		"redirect_uri":          {redirectURI},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"scope":                 {DefaultOAuthScope},
		"state":                 {state},
	}
	var response authorizationResponse
	if err := s.postJSON(ctx, "/v1/developers/me/oauth-authorize?"+query.Encode(), loginToken, map[string]bool{"confirm": true}, &response); err != nil {
		return "", err
	}

	redirect, err := url.Parse(response.RedirectURI)
	if err != nil {
		return "", fmt.Errorf("invalid authorization redirect: %w", err)
	}
	expected, err := url.Parse(redirectURI)
	if err != nil {
		return "", fmt.Errorf("invalid callback URL: %w", err)
	}
	if redirect.Scheme != expected.Scheme || redirect.Host != expected.Host || redirect.Path != expected.Path {
		return "", fmt.Errorf("authorization redirect did not match the local callback")
	}
	if redirect.Query().Get("state") != state {
		return "", fmt.Errorf("authorization state mismatch")
	}
	code := redirect.Query().Get("code")
	if code == "" {
		return "", fmt.Errorf("authorization redirect did not include a code")
	}
	return code, nil
}

func (s *OAuthService) LogoutLoginToken(ctx context.Context, loginToken string) error {
	return s.postJSON(ctx, "/v1/developers/logout", loginToken, struct{}{}, nil)
}

// Refresh exchanges a refresh token for a new token pair.
func (s *OAuthService) Refresh(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	return s.postToken(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {s.clientID},
	})
}

func (s *OAuthService) postToken(ctx context.Context, body url.Values) (*TokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.baseURL+"/oauth2/token",
		strings.NewReader(body.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var e struct {
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&e)
		if e.ErrorDescription != "" {
			return nil, fmt.Errorf("token request failed: %s", e.ErrorDescription)
		}
		return nil, fmt.Errorf("token request failed (HTTP %d)", resp.StatusCode)
	}

	var tr TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, fmt.Errorf("decoding token response: %w", err)
	}
	return &tr, nil
}

func (s *OAuthService) postJSON(ctx context.Context, path, bearerToken string, body, destination any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+path, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var response struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&response)
		if response.Message != "" {
			return fmt.Errorf("request failed: %s", response.Message)
		}
		if response.Code != "" {
			return fmt.Errorf("request failed: %s", response.Code)
		}
		return fmt.Errorf("request failed (HTTP %d)", resp.StatusCode)
	}
	if destination == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(destination); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}
	return nil
}

// GeneratePKCE returns a (verifier, challenge) pair for the S256 method.
// verifier is 43 URL-safe chars; challenge is BASE64URL(SHA256(verifier)).
func GeneratePKCE() (verifier, challenge string, err error) {
	b := make([]byte, 32) // 32 bytes → 43 base64url chars (no padding)
	if _, err = rand.Read(b); err != nil {
		return
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return
}
