package cli

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/config"
	"github.com/revenuecat/cli/internal/tui"
)

const (
	loginMethodOAuth  = "oauth"
	loginMethodAPIKey = "api_key"
)

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authenticate and manage credentials",
	}
	cmd.AddCommand(
		newAuthLoginCmd(),
		newAuthLogoutCmd(),
		newAuthStatusCmd(),
	)
	return cmd
}

func newAuthLoginCmd() *cobra.Command {
	var apiKey string
	var useOAuth bool

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with RevenueCat",
		Long: `Authenticates and stores credentials in the active profile
(~/.config/revenuecat/<profile>.json, mode 0600).

Two login methods are available:

  Browser login  Opens a browser window for OAuth authorization. Tokens are
                 stored and refreshed automatically.

  API key        Paste a secret key from the dashboard. The key is stored
                 as-is and never refreshed.

The API key can also be supplied via RC_API_KEY for CI use without storing
anything on disk.`,
		Example: `  # Interactive — prompts for method
  rc auth login

  # Browser OAuth (non-interactive)
  rc auth login --oauth

  # API key (non-interactive)
  rc auth login --api-key sk_...

  # CI: don't store on disk
  RC_API_KEY=sk_... rc customer list`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := RuntimeFrom(cmd.Context())

			if apiKey != "" {
				return loginWithAPIKey(cmd.Context(), rt, apiKey)
			}
			if useOAuth {
				return loginWithOAuth(cmd.Context(), rt)
			}
			if rt.Globals.NoInput {
				return fmt.Errorf("pass --api-key or set RC_API_KEY for non-interactive login")
			}

			var method string
			sel := huh.NewSelect[string]().
				Title("Login method").
				Options(
					huh.NewOption("Log in with RevenueCat  (opens browser)", loginMethodOAuth),
					huh.NewOption("API key  (paste from dashboard)", loginMethodAPIKey),
				).
				Value(&method)
			if err := tui.Form(false).Field(sel).Run(); err != nil {
				return err
			}

			switch method {
			case loginMethodOAuth:
				return loginWithOAuth(cmd.Context(), rt)
			default:
				return loginWithAPIKeyInteractive(cmd.Context(), rt)
			}
		},
	}

	cmd.Flags().StringVar(&apiKey, "api-key", "", "RevenueCat API key (or set RC_API_KEY)")
	cmd.Flags().BoolVar(&useOAuth, "oauth", false, "log in via browser OAuth flow")
	return cmd
}

func newAuthLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove stored credentials from the active profile",
		Long: `Clears the API key and OAuth tokens from the active profile file.
The profile file itself is kept; only the auth fields are zeroed.

To remove the profile entirely, use: rc profiles delete <name>`,
		Example: `  rc auth logout
  rc auth logout --profile staging`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := RuntimeFrom(cmd.Context())

			rt.Config.APIKey = ""
			rt.Config.AccessToken = ""
			rt.Config.RefreshToken = ""
			rt.Config.TokenExpiresAt = time.Time{}
			rt.Config.TokenType = ""

			if err := config.Save(rt.Globals.Profile, rt.Config); err != nil {
				return err
			}

			profileName := config.ProfileName(rt.Globals.Profile)
			rt.Out.Success(fmt.Sprintf("Logged out (profile: %s)", profileName))
			return rt.Out.Render(map[string]any{
				"profile": profileName,
			})
		},
	}
}

func newAuthStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the current authentication state",
		Long:  `Displays the active profile, auth method, and project context. Does not make any API calls.`,
		Example: `  rc auth status
  rc auth status --json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := RuntimeFrom(cmd.Context())

			profileName := config.ProfileName(rt.Globals.Profile)
			authenticated := rt.Config.BearerToken() != ""

			var method string
			switch {
			case rt.Config.IsOAuth():
				if rt.Config.TokenExpiresAt.IsZero() {
					method = "oauth"
				} else if time.Now().After(rt.Config.TokenExpiresAt) {
					method = "oauth (expired)"
				} else {
					method = fmt.Sprintf("oauth (expires %s)", rt.Config.TokenExpiresAt.Local().Format("2006-01-02 15:04"))
				}
			case rt.Config.APIKey != "":
				method = "api_key"
			default:
				method = "none"
			}

			if !authenticated {
				rt.Out.Info(fmt.Sprintf("Not logged in (profile: %s) — run `rc auth login`", profileName))
			} else {
				rt.Out.Success(fmt.Sprintf("Logged in (profile: %s)", profileName))
			}

			return rt.Out.Render(map[string]any{
				"profile":       profileName,
				"authenticated": authenticated,
				"method":        method,
				"project_id":    rt.Config.ProjectID,
				"base_url":      rt.Config.BaseURL,
			})
		},
	}
}

func loginWithAPIKeyInteractive(ctx context.Context, rt *Runtime) error {
	rt.Out.Info("Generate an API key at https://app.revenuecat.com/settings/api-keys")
	var key string
	if err := tui.Form(rt.Globals.NoInput).
		Field(huh.NewInput().
			Title("RevenueCat API key").
			EchoMode(huh.EchoModePassword).
			Value(&key).
			Validate(tui.Required("API key"))).
		Run(); err != nil {
		return err
	}
	return loginWithAPIKey(ctx, rt, key)
}

func loginWithAPIKey(ctx context.Context, rt *Runtime, key string) error {
	rt.Config.APIKey = key
	rt.Config.TokenType = ""
	rt.Config.AccessToken = ""
	rt.Config.RefreshToken = ""
	rt.Config.TokenExpiresAt = time.Time{}

	client, err := rt.API()
	if err != nil {
		return err
	}
	return finishLogin(ctx, rt, client)
}

func loginWithOAuth(ctx context.Context, rt *Runtime) error {
	verifier, challenge, err := api.GeneratePKCE()
	if err != nil {
		return fmt.Errorf("generating PKCE: %w", err)
	}
	state, err := api.GenerateState()
	if err != nil {
		return fmt.Errorf("generating state: %w", err)
	}

	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return fmt.Errorf("starting callback server: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://localhost:%d/callback", port)

	svc := api.NewOAuthService(oauthBaseURL(), oauthClientID())
	authURL := svc.AuthorizeURL(redirectURI, challenge, state)

	rt.Out.Info("Opening browser for authorization…")
	rt.Out.Info(fmt.Sprintf("If the browser doesn't open, visit:\n  %s", authURL))

	_ = openBrowser(authURL)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if e := r.URL.Query().Get("error"); e != "" {
			desc := r.URL.Query().Get("error_description")
			fmt.Fprintf(w, "<html><body><p>Authorization failed: %s %s — you may close this tab.</p></body></html>", e, desc)
			errCh <- fmt.Errorf("authorization denied: %s %s", e, desc)
			return
		}
		if got := r.URL.Query().Get("state"); got != state {
			fmt.Fprintf(w, "<html><body><p>Invalid state — you may close this tab.</p></body></html>")
			errCh <- fmt.Errorf("state mismatch (possible CSRF); please try logging in again")
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			fmt.Fprintf(w, "<html><body><p>Missing code — you may close this tab.</p></body></html>")
			errCh <- fmt.Errorf("callback missing 'code' parameter")
			return
		}
		fmt.Fprintf(w, "<html><body><p>Authorization successful — you may close this tab.</p></body></html>")
		codeCh <- code
	})

	srv := &http.Server{Handler: mux}
	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("callback server error: %w", err)
		}
	}()
	defer srv.Close()

	var code string
	select {
	case code = <-codeCh:
	case err = <-errCh:
		return err
	case <-time.After(5 * time.Minute):
		return fmt.Errorf("timed out waiting for browser authorization (5 min)")
	case <-ctx.Done():
		return ctx.Err()
	}

	rt.Out.Info("Authorization received — exchanging code for tokens…")

	tr, err := svc.ExchangeCode(ctx, code, redirectURI, verifier)
	if err != nil {
		return fmt.Errorf("token exchange: %w", err)
	}

	rt.Config.TokenType = "oauth"
	rt.Config.AccessToken = tr.AccessToken
	rt.Config.RefreshToken = tr.RefreshToken
	rt.Config.TokenExpiresAt = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	rt.Config.APIKey = ""

	rt.client = nil
	client, err := rt.API()
	if err != nil {
		return err
	}
	return finishLogin(ctx, rt, client)
}

func finishLogin(ctx context.Context, rt *Runtime, _ *api.Client) error {
	if err := config.Save(rt.Globals.Profile, rt.Config); err != nil {
		return err
	}
	rt.Out.Success(fmt.Sprintf("Logged in (profile: %s)", config.ProfileName(rt.Globals.Profile)))
	rt.Out.Info("Run `rc projects use` to set a default project, or you'll be prompted on first use.")
	return rt.Out.Render(map[string]any{
		"profile": config.ProfileName(rt.Globals.Profile),
	})
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
