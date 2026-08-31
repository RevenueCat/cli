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
	"github.com/revenuecat/cli/internal/buildinfo"
	"github.com/revenuecat/cli/internal/config"
	"github.com/revenuecat/cli/internal/httpx"
	"github.com/revenuecat/cli/internal/output"
	"github.com/revenuecat/cli/internal/tui"
)

// usageError marks bad user input (bad flags) so ExitCodeFor returns the
// conventional exit 2. Flag errors are wrapped with it via the root's
// FlagErrorFunc; cobra's own unknown-command / arg-count errors don't pass
// through that hook, so ExitCodeFor also matches their stable messages.
type usageError struct{ err error }

func (e usageError) Error() string { return e.err.Error() }
func (e usageError) Unwrap() error { return e.err }

// unknownSubcommandError is returned when a command group is handed a verb it
// doesn't have. It carries a distinct type so the JSON envelope can label it
// unknown_command_error while still mapping to the usage exit code.
type unknownSubcommandError struct {
	parent     string
	name       string
	suggestion string
}

func (e *unknownSubcommandError) Error() string {
	msg := fmt.Sprintf("unknown command %q for %q", e.name, e.parent)
	if e.suggestion != "" {
		msg += fmt.Sprintf("; did you mean %q?", e.suggestion)
	}
	return msg
}

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

	client             *api.Client
	warnedCredConflict bool
}

// CanPrompt reports whether it's safe to show an interactive prompt: not in
// --json or --no-input mode, and attached to a TTY.
func (rt *Runtime) CanPrompt() bool {
	return !rt.Globals.JSON && !rt.Globals.NoInput && tui.IsInteractive()
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

	// Refresh only when the OAuth token is the credential this run will use —
	// a flag or RC_API_KEY override shouldn't touch (or re-save) the profile.
	if _, source := r.Config.Credential(); source == config.SourceOAuth && r.Config.NeedsRefresh() {
		r.silentRefresh()
	}

	token, source := r.Config.Credential()
	if token == "" {
		return nil, ErrNotAuthenticated
	}
	r.warnCredentialConflict(source)
	baseURL := r.effectiveBaseURL()
	if baseURL != "" {
		if u, err := url.Parse(baseURL); err != nil || u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("invalid base URL %q (RC_BASE_URL or profile base_url): expected an absolute URL like https://api.revenuecat.com/v2", baseURL)
		}
	}
	r.client = api.NewClient(api.Options{
		APIKey:           token, // works for both API keys and OAuth tokens
		CredentialSource: string(source),
		BaseURL:          baseURL,
		UserAgent:        userAgent(r.Globals.Version),
		ExtraHeaders:     requestHeaders(r.Globals),
	})
	return r.client, nil
}

// effectiveBaseURL is the configured base URL in dev builds and empty in release
// builds, so a shipped binary always talks to the production endpoints regardless
// of RC_BASE_URL or profile base_url. Use it anywhere a credential or key is sent.
func (r *Runtime) effectiveBaseURL() string {
	if buildinfo.IsDev() {
		return r.Config.BaseURL
	}
	return ""
}

// warnCredentialConflict warns once per run when more than one credential source is present.
func (r *Runtime) warnCredentialConflict(active config.CredentialSource) {
	if r.warnedCredConflict {
		return
	}
	present := r.Config.PresentCredentialSources()
	if len(present) < 2 {
		return
	}
	r.warnedCredConflict = true
	r.Out.AlwaysWarn(credentialConflictMessage(r.Config, active, present))
}

func credentialConflictMessage(cfg *config.Config, active config.CredentialSource, present []config.CredentialSource) string {
	var ignored []string
	for _, s := range present {
		if s != active {
			ignored = append(ignored, cfg.DescribeSource(s))
		}
	}
	msg := "Multiple credentials found — using " + cfg.DescribeSource(active)
	if len(ignored) > 0 {
		msg += "; ignoring " + strings.Join(ignored, " and ")
	}
	return msg + "."
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
	// Dev-only override, like the other endpoints: a release binary always
	// authenticates against the production OAuth host.
	if buildinfo.IsDev() {
		if v := os.Getenv("RC_OAUTH_BASE_URL"); v != "" {
			return v
		}
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
	return fmt.Sprintf("revenuecat-cli/%s (%s %s; go%s)", version, runtime.GOOS, runtime.GOARCH, strings.TrimPrefix(runtime.Version(), "go"))
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
	var uce *unknownSubcommandError
	if errors.As(err, &ue) || errors.As(err, &uce) || isCobraUsage(err) {
		return 2
	}
	switch {
	case api.IsUnauthorized(err):
		return 4
	case api.IsNotFound(err):
		return 5
	case api.IsRateLimited(err):
		return 6
	}
	if errors.Is(err, ErrNotAuthenticated) {
		return 4
	}
	return 1
}
