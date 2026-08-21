// Package google bootstraps the Google-side credential RevenueCat needs for a
// Google Play app: it drives a local (loopback + PKCE) Google OAuth sign-in and
// then uses that human authority to create a narrowly scoped service account,
// its key, and the Play Console grants. The human's OAuth token stays in
// memory and is never sent to RevenueCat.
//
// This package is service-only — no CLI concepts — mirroring internal/appleconnect.
package google

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// OAuth scopes. Request the minimum: androidpublisher for Play users/grants,
// cloud-platform for the GCP bootstrap, and openid/email so we can show who
// signed in. All are "sensitive" (not "restricted") to Google — verification
// is required for >100 users but not the CASA security assessment.
const (
	ScopeAndroidPublisher = "https://www.googleapis.com/auth/androidpublisher"
	ScopeCloudPlatform    = "https://www.googleapis.com/auth/cloud-platform"
	ScopePlayReporting    = "https://www.googleapis.com/auth/playdeveloperreporting"
	scopeOpenID           = "openid"
	scopeEmail            = "https://www.googleapis.com/auth/userinfo.email"
)

// DefaultScopes covers the whole setup flow. playdeveloperreporting is what
// lets us list the developer's Play apps so they don't have to type a package.
var DefaultScopes = []string{ScopeAndroidPublisher, ScopeCloudPlatform, ScopePlayReporting, scopeOpenID, scopeEmail}

// DefaultClientID / DefaultClientSecret are the RevenueCat-owned Desktop OAuth
// client baked into the binary. They are intentionally empty until RevenueCat
// provisions the official client; RC_GOOGLE_CLIENT_ID / RC_GOOGLE_CLIENT_SECRET
// override them (and let developers test against their own Desktop client under
// Google's 100-user unverified cap in the meantime). A Desktop client secret is
// not confidential — PKCE is the real protection — so shipping it is expected.
var (
	DefaultClientID     = ""
	DefaultClientSecret = ""
)

// ErrNoClientID is returned when neither a baked-in nor an env-provided client
// ID is available. The message tells the human how to create one.
var ErrNoClientID = errors.New(
	"no Google OAuth client configured: set RC_GOOGLE_CLIENT_ID (and RC_GOOGLE_CLIENT_SECRET) " +
		"to a Google Cloud \"Desktop app\" OAuth client. Create one at " +
		"https://console.cloud.google.com/apis/credentials → Create credentials → OAuth client ID → Desktop app")

// BrowserFunc opens a URL in the user's browser. Injected so the CLI can reuse
// its own openBrowser and tests can stub it.
type BrowserFunc func(url string) error

// Credentials is the result of a successful sign-in: a refreshing TokenSource
// to authorize every subsequent API client, and the signed-in email for display.
type Credentials struct {
	TokenSource oauth2.TokenSource
	Email       string
}

// Authenticate runs the Desktop-app OAuth flow: it starts a loopback listener
// on 127.0.0.1, opens the browser to Google's consent screen with a PKCE
// challenge, captures the redirect, and exchanges the code for tokens. The
// returned TokenSource auto-refreshes.
func Authenticate(ctx context.Context, clientID, clientSecret string, scopes []string, open BrowserFunc) (*Credentials, error) {
	if clientID == "" {
		return nil, ErrNoClientID
	}
	if len(scopes) == 0 {
		scopes = DefaultScopes
	}

	// 127.0.0.1 (not "localhost") — Google recommends the literal loopback IP
	// to avoid client-firewall issues. Port 0 asks the OS for a free port.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("start loopback listener: %w", err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port

	conf := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     google.Endpoint,
		RedirectURL:  fmt.Sprintf("http://127.0.0.1:%d/callback", port),
		Scopes:       scopes,
	}

	verifier := oauth2.GenerateVerifier()
	state, err := randomState()
	if err != nil {
		return nil, err
	}
	authURL := conf.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.ApprovalForce, // always return a refresh token
		oauth2.S256ChallengeOption(verifier))

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if e := q.Get("error"); e != "" {
			writeBrowserMessage(w, "Authorization failed — you may close this tab.")
			sendErr(errCh, fmt.Errorf("authorization denied: %s %s", e, q.Get("error_description")))
			return
		}
		if q.Get("state") != state {
			writeBrowserMessage(w, "Invalid state — you may close this tab.")
			sendErr(errCh, errors.New("state mismatch (possible CSRF); please try again"))
			return
		}
		code := q.Get("code")
		if code == "" {
			writeBrowserMessage(w, "Missing code — you may close this tab.")
			sendErr(errCh, errors.New("callback missing 'code' parameter"))
			return
		}
		writeBrowserMessage(w, "Signed in to Google — you may close this tab and return to your terminal.")
		select {
		case codeCh <- code:
		default:
		}
	})

	server := &http.Server{Handler: mux}
	go func() {
		if serveErr := server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			sendErr(errCh, serveErr)
		}
	}()
	defer server.Close()

	if open != nil {
		_ = open(authURL)
	}

	var code string
	select {
	case code = <-codeCh:
	case err = <-errCh:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	token, err := conf.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, fmt.Errorf("exchange authorization code: %w", err)
	}

	ts := conf.TokenSource(ctx, token)
	email, err := fetchEmail(ctx, oauth2.NewClient(ctx, ts))
	if err != nil {
		// Non-fatal: the setup can proceed without a display email.
		email = ""
	}
	return &Credentials{TokenSource: ts, Email: email}, nil
}

// AuthURL is the loopback URL to print when the browser can't open. It is not
// used in the happy path but kept for parity with the CLI's other OAuth flow.

func fetchEmail(ctx context.Context, client *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://openidconnect.googleapis.com/v1/userinfo", nil)
	if err != nil {
		return "", err
	}
	client.Timeout = 15 * time.Second
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("userinfo returned status %d", resp.StatusCode)
	}
	var payload struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	return strings.TrimSpace(payload.Email), nil
}

func writeBrowserMessage(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<html><body style=\"font-family:system-ui;padding:2rem\"><p>%s</p></body></html>", msg)
}

func sendErr(ch chan<- error, err error) {
	select {
	case ch <- err:
	default:
	}
}
