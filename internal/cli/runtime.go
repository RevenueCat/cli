package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/config"
	"github.com/revenuecat/cli/internal/httpx"
	"github.com/revenuecat/cli/internal/output"
)

// usageError marks bad user input (bad flags) so ExitCodeFor returns the
// conventional exit 2. Flag errors are wrapped with it via the root's
// FlagErrorFunc; cobra's own unknown-command / arg-count errors don't pass
// through that hook, so ExitCodeFor also matches their stable messages.
type usageError struct{ err error }

func (e usageError) Error() string { return e.err.Error() }
func (e usageError) Unwrap() error { return e.err }

// cobra emits these prefixes for command/arg misuse; none pass through
// FlagErrorFunc, so match them by message.
func isCobraUsage(err error) bool {
	msg := err.Error()
	for _, p := range []string{"unknown command", "accepts ", "requires "} {
		if strings.HasPrefix(msg, p) {
			return true
		}
	}
	return false
}

type runtimeKey struct{}

type Runtime struct {
	Globals *Globals
	Config  *config.Config
	Out     *output.Renderer
	Ctx     context.Context

	client *api.Client
}

func WithRuntime(ctx context.Context, r *Runtime) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, runtimeKey{}, r)
}

func RuntimeFrom(ctx context.Context) *Runtime {
	r, _ := ctx.Value(runtimeKey{}).(*Runtime)
	return r
}

// API returns a lazily-initialized API client built from the active config.
// If the profile holds an OAuth token that is near expiry, a silent refresh
// is attempted before building the client.
func (r *Runtime) API() (*api.Client, error) {
	if r.client != nil {
		return r.client, nil
	}

	if r.Config.NeedsRefresh() {
		r.silentRefresh()
	}

	if r.Config.BearerToken() == "" {
		return nil, ErrNotAuthenticated
	}
	if b := r.Config.BaseURL; b != "" {
		if u, err := url.Parse(b); err != nil || u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("invalid base URL %q (RC_BASE_URL or profile base_url): expected an absolute URL like https://api.revenuecat.com/v2", b)
		}
	}
	r.client = api.NewClient(api.Options{
		APIKey:       r.Config.BearerToken(), // works for both API keys and OAuth tokens
		BaseURL:      r.Config.BaseURL,
		UserAgent:    userAgent(r.Globals.Version),
		ExtraHeaders: customHeaders(),
	})
	return r.client, nil
}

func customHeaders() http.Header {
	return httpx.ParseHeaders(os.Getenv("RC_HEADERS"))
}

// silentRefresh attempts to refresh the OAuth token without surfacing errors —
// if the refresh fails the caller will get a 401 on the next request, which
// maps to exit 4 and prompts the user to re-login.
//
// Not goroutine-safe: the CLI is single-threaded by design; do not call from
// concurrent goroutines without adding a mutex to Runtime.
func (r *Runtime) silentRefresh() {
	svc := api.NewOAuthService(oauthBaseURL(), oauthClientID())
	tr, err := svc.Refresh(r.Ctx, r.Config.RefreshToken)
	if err != nil {
		return
	}
	r.Config.AccessToken = tr.AccessToken
	r.Config.RefreshToken = tr.RefreshToken
	r.Config.TokenExpiresAt = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	_ = config.Save(r.Globals.Profile, r.Config)
}

func oauthBaseURL() string {
	if v := os.Getenv("RC_OAUTH_BASE_URL"); v != "" {
		return v
	}
	return api.DefaultOAuthBaseURL
}

func oauthClientID() string {
	if v := os.Getenv("RC_OAUTH_CLIENT_ID"); v != "" {
		return v
	}
	return api.DefaultOAuthClientID
}

func userAgent(version string) string {
	if version == "" {
		version = "dev"
	}
	return fmt.Sprintf("revenuecat-cli/%s (%s/%s)", version, runtime.GOOS, runtime.GOARCH)
}

var ErrNotAuthenticated = errors.New("not authenticated: run `rc login` or pass --api-key / set RC_API_KEY")

// SilentExitError signals a specific exit code when the command has already
// written its own complete output. run.go skips the error envelope for this
// type so there is no duplicate output.
type SilentExitError struct{ Code int }

func (e *SilentExitError) Error() string { return "" }

func ExitCodeFor(err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, output.ErrBadFormat) {
		return 2
	}
	var ue usageError
	if errors.As(err, &ue) || isCobraUsage(err) {
		return 2
	}
	var apiErr *api.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.Type {
		case "unauthorized", "authentication_error":
			return 4
		case "resource_missing":
			return 5
		case "rate_limit_exceeded":
			return 6
		}
	}
	if errors.Is(err, ErrNotAuthenticated) {
		return 4
	}
	return 1
}
