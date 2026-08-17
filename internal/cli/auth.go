package cli

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
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
	passwordCreate    = "create"
	passwordGenerate  = "generate"
)

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authenticate and manage credentials",
	}
	cmd.AddCommand(
		newAuthLoginCmd(),
		newAuthSignupCmd(),
		newAuthLogoutCmd(),
		newAuthStatusCmd(),
	)
	return cmd
}

func newAuthSignupCmd() *cobra.Command {
	var email string
	var name string
	var acceptTerms bool
	var marketingEmails bool
	var password string
	var generatePassword bool
	var savePassword bool

	cmd := &cobra.Command{
		Use:   "signup",
		Short: "Create a RevenueCat account and log in",
		Long: `Creates a RevenueCat account without opening a browser, then converts the
temporary signup session into the same renewable OAuth credentials used by
browser login.

Interactive signup lets you create a password or generate a strong random one.
On macOS, rc can save it as an app.revenuecat.com internet password in the local
login Keychain with your explicit approval. Standalone CLIs cannot add credentials
to the entitlement-protected Apple Passwords/iCloud Keychain. Passwords are sent
only to RevenueCat over HTTPS and are never printed. Renewable OAuth tokens are
saved in the active local profile (~/.config/revenuecat/<profile>.json, mode 0600).

You must accept the RevenueCat Terms of Service and Privacy Policy:
  https://www.revenuecat.com/terms
  https://www.revenuecat.com/privacy`,
		Example: `  # Interactive signup
  rc auth signup

  # Agent helping a user sign up (--save-password saves to the macOS Keychain), after they agreed to the Terms
  rc auth signup --email dev@example.com --name "Example Developer" \
    --generate-password --save-password --accept-terms --no-input --json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := RuntimeFrom(cmd.Context())
			email = strings.TrimSpace(email)
			name = strings.TrimSpace(name)
			if password == "" {
				password = os.Getenv("RC_PASSWORD")
			}
			if password != "" && generatePassword {
				return fmt.Errorf("pass either --password or --generate-password, not both")
			}
			if savePassword && runtime.GOOS != "darwin" {
				return fmt.Errorf("--save-password is currently supported only on macOS")
			}

			if rt.Globals.NoInput {
				var missing []string
				if email == "" {
					missing = append(missing, "--email")
				}
				if name == "" {
					missing = append(missing, "--name")
				}
				if !acceptTerms {
					missing = append(missing, "--accept-terms")
				}
				if len(missing) > 0 {
					return fmt.Errorf("missing required flags for non-interactive signup: %s", strings.Join(missing, ", "))
				}
			} else {
				form := tui.Form(false)
				if email == "" {
					form.Field(huh.NewInput().Title("Email").Value(&email).Validate(tui.Required("Email")))
				}
				if name == "" {
					form.Field(huh.NewInput().Title("Your name").Description("Your personal/display name; project naming happens after signup").Value(&name).Validate(tui.Required("Name")))
				}
				if !marketingEmails {
					form.Field(huh.NewConfirm().Title("Receive RevenueCat product updates?").Value(&marketingEmails))
				}
				if err := form.Run(); err != nil {
					return err
				}
				rt.Out.Answer("Email", email)
				rt.Out.Answer("Name", name)
				rt.Out.Answer("Product updates", map[bool]string{true: "yes", false: "no"}[marketingEmails])
				if password == "" && !generatePassword {
					var passwordMode string
					if err := tui.Form(false).Field(huh.NewSelect[string]().
						Title("Account password").
						Options(
							huh.NewOption("Create my own password", passwordCreate),
							huh.NewOption("Generate a strong random password", passwordGenerate),
						).
						Value(&passwordMode)).Run(); err != nil {
						return err
					}
					generatePassword = passwordMode == passwordGenerate
					rt.Out.Answer("Password", map[bool]string{true: "generate", false: "create my own"}[generatePassword])
					if generatePassword {
						note := `You can set your own anytime with "Forgot password?" at revenuecat.com.`
						if runtime.GOOS == "darwin" {
							note = "We'll offer to save it to your macOS Keychain. " + note
						}
						rt.Out.Info(note)
					}
				}
				if password == "" && !generatePassword {
					var confirmation string
					if err := tui.Form(false).
						Field(huh.NewInput().Title("Create password").EchoMode(huh.EchoModePassword).Value(&password).Validate(validateSignupPassword)).
						Field(huh.NewInput().Title("Confirm password").EchoMode(huh.EchoModePassword).Value(&confirmation).Validate(func(value string) error {
							if value != password {
								return fmt.Errorf("passwords do not match")
							}
							return nil
						})).Run(); err != nil {
						return err
					}
				}
				if runtime.GOOS == "darwin" && !savePassword {
					confirmed, err := tui.ConfirmDefault(false, "Save the password in your macOS Keychain?", true)
					if err != nil {
						return err
					}
					savePassword = confirmed
					rt.Out.Answer("Keychain", map[bool]string{true: "save password", false: "don't save"}[savePassword])
				}
				if !acceptTerms {
					rt.Out.Hint("Terms: https://www.revenuecat.com/terms · Privacy: https://www.revenuecat.com/privacy")
					confirmed, err := tui.Confirm(false, "Accept the Terms of Service and Privacy Policy, and create the account now?")
					if err != nil {
						return err
					}
					if !confirmed {
						return fmt.Errorf("signup requires accepting the Terms of Service and Privacy Policy")
					}
					rt.Out.Blank()
				}
			}

			if password == "" {
				passwordBytes := make([]byte, 32)
				if _, err := rand.Read(passwordBytes); err != nil {
					return fmt.Errorf("generating signup credential: %w", err)
				}
				password = base64.RawURLEncoding.EncodeToString(passwordBytes)
				generatePassword = true
			}
			if err := validateSignupPassword(password); err != nil {
				return err
			}
			return signupWithOAuth(cmd.Context(), rt, email, name, password, marketingEmails, savePassword, generatePassword)
		},
	}

	cmd.Flags().StringVar(&email, "email", "", "email address for the new RevenueCat account")
	cmd.Flags().StringVar(&name, "name", "", "your personal/display name (not the project or company name)")
	cmd.Flags().StringVar(&password, "password", "", "account password (prefer RC_PASSWORD to avoid shell history)")
	cmd.Flags().BoolVar(&generatePassword, "generate-password", false, "generate a strong random account password")
	cmd.Flags().BoolVar(&savePassword, "save-password", false, "save the website password in the local macOS login Keychain; does not sync to Apple Passwords/iCloud")
	cmd.Flags().BoolVar(&acceptTerms, "accept-terms", false, "accept the RevenueCat Terms of Service and Privacy Policy")
	cmd.Flags().BoolVar(&marketingEmails, "marketing-emails", false, "receive RevenueCat product and marketing emails")
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
  RC_API_KEY=sk_... rc customers list`,
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

			// A RevenueCat credential already in an agent's MCP config is
			// offered as a fast start (skips the browser). It's opaque, so we
			// can't tell if it's still alive here — validation happens when
			// it's used, and a dead one nudges to browser login.
			found := discoverMCPCredentials()
			options := make([]huh.Option[string], 0, len(found)+2)
			for i, c := range found {
				options = append(options, huh.NewOption(c.label(), fmt.Sprintf("mcp:%d", i)))
			}
			options = append(options,
				huh.NewOption("Log in with RevenueCat  (opens browser)", loginMethodOAuth),
				huh.NewOption("API key  (paste from dashboard)", loginMethodAPIKey),
			)

			var method string
			if err := tui.Form(false).
				Field(huh.NewSelect[string]().Title("Login method").Options(options...).Value(&method)).
				Run(); err != nil {
				return err
			}
			if strings.HasPrefix(method, "mcp:") {
				var i int
				// method comes only from the Select above, so the index is
				// always valid today — but validate anyway so a future flag or
				// menu change can't turn this into an out-of-bounds panic.
				if n, _ := fmt.Sscanf(method, "mcp:%d", &i); n != 1 || i < 0 || i >= len(found) {
					return fmt.Errorf("invalid MCP credential selection %q", method)
				}
				rt.Out.Answer("Login method", "imported from "+found[i].Source+" MCP config")
				return loginWithMCPCredential(cmd.Context(), rt, found[i])
			}
			rt.Out.Answer("Login method", map[string]string{loginMethodOAuth: "browser (RevenueCat account)", loginMethodAPIKey: "API key"}[method])

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
			rt.Config.AccountEmail = ""
			rt.Config.AccountName = ""
			rt.Config.AuthSource = ""
			rt.Config.ProjectID = ""

			if err := config.Save(rt.Globals.Profile, rt.Config); err != nil {
				return err
			}

			profileName := config.ProfileName(rt.Globals.Profile)
			rt.Out.Success(fmt.Sprintf("Logged out (profile: %s)", profileName))
			if !rt.Out.IsJSON() {
				return nil
			}
			return rt.Out.Render(map[string]any{
				"profile": profileName,
			})
		},
	}
}

func newAuthStatusCmd() *cobra.Command {
	var showScopes bool
	cmd := &cobra.Command{
		Use:     "status",
		Aliases: []string{"whoami"},
		Short:   "Show the current authentication state",
		Long: `Displays the active profile, cached account identity when known, auth method, and project context. If a project is configured, validates that it is still accessible.

Shows which credential is in control (the OAuth login, an RC_API_KEY env var, the --api-key flag, or a stored key) and warns when more than one is present. Pass --scopes to surface the active credential's scopes before attempting writes.`,
		Example: `  rc auth status
  rc auth status --json
  rc auth status --scopes --json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := RuntimeFrom(cmd.Context())

			profileName := config.ProfileName(rt.Globals.Profile)
			token, credSource := rt.Config.Credential()
			authenticated := token != ""
			projectStatus := "not_configured"
			if authenticated && rt.Config.ProjectID != "" {
				projectStatus = "unavailable"
				if client, err := rt.API(); err == nil {
					if _, err := client.Projects.Get(cmd.Context(), rt.Config.ProjectID); err == nil {
						projectStatus = "valid"
					} else if apiErr, ok := err.(*api.APIError); ok && apiErr.Status == http.StatusNotFound {
						projectStatus = "not_found"
					}
				}
			}

			var method string
			switch credSource {
			case config.SourceOAuth:
				if rt.Config.TokenExpiresAt.IsZero() {
					method = "oauth"
				} else if time.Now().After(rt.Config.TokenExpiresAt) {
					method = "oauth (expired)"
				} else {
					method = fmt.Sprintf("oauth (expires %s)", rt.Config.TokenExpiresAt.Local().Format("2006-01-02 15:04"))
				}
			case config.SourceFlag, config.SourceEnv, config.SourceProfile:
				method = "api_key"
			default:
				method = "none"
			}

			present := rt.Config.PresentCredentialSources()
			var conflict map[string]any
			if len(present) > 1 {
				ignored := make([]string, 0, len(present)-1)
				for _, s := range present {
					if s != credSource {
						ignored = append(ignored, string(s))
					}
				}
				conflict = map[string]any{
					"active_source":   string(credSource),
					"ignored_sources": ignored,
					"message":         credentialConflictMessage(credSource, present),
				}
			}

			var scopes []string
			scopesKnown := false
			if showScopes && authenticated {
				scopes, scopesKnown = credentialScopes(token)
			}

			identity := rt.Config.AccountEmail
			if rt.Config.AccountName != "" && identity != "" {
				identity = fmt.Sprintf("%s <%s>", rt.Config.AccountName, identity)
			} else if rt.Config.AccountName != "" {
				identity = rt.Config.AccountName
			}

			if !authenticated {
				rt.Out.Info(fmt.Sprintf("Not logged in (profile: %s)", profileName))
				rt.Out.Hint("Log in:  rc auth login")
			} else if identity != "" {
				rt.Out.Success(fmt.Sprintf("Logged in as %s (profile: %s)", identity, profileName))
			} else {
				rt.Out.Success(fmt.Sprintf("Logged in (profile: %s)", profileName))
			}
			if authenticated {
				rt.Out.Field("Credential", rt.Config.CredentialDescription())
				if credSource == config.SourceOAuth {
					writeTokenStatus(rt)
				}
			}
			if conflict != nil {
				rt.warnCredentialConflict(credSource)
			}
			if showScopes && authenticated {
				if scopesKnown {
					rt.Out.Field("Scopes", strings.Join(scopes, ", "))
				} else {
					rt.Out.Field("Scopes", "unknown")
					rt.Out.Hint("Scopes can't be read locally for this credential; the API exposes no token-introspection endpoint yet. A write that needs a missing scope will name it.")
				}
			}
			if projectStatus == "not_found" {
				rt.Out.Warn(fmt.Sprintf("Configured project %s is no longer accessible; run `rc projects use`", rt.Config.ProjectID))
			} else if projectStatus == "unavailable" {
				rt.Out.Info(fmt.Sprintf("Could not validate configured project %s", rt.Config.ProjectID))
			}

			if !rt.Out.IsJSON() {
				return nil
			}
			out := map[string]any{
				"profile":                profileName,
				"authenticated":          authenticated,
				"account_email":          rt.Config.AccountEmail,
				"account_name":           rt.Config.AccountName,
				"method":                 method,
				"credential_source":      string(credSource),
				"credential_description": rt.Config.CredentialDescription(),
				"project_id":             rt.Config.ProjectID,
				"project_status":         projectStatus,
				"base_url":               rt.Config.BaseURL,
			}
			if authenticated && rt.Config.AuthSource != "" {
				out["auth_origin"] = rt.Config.AuthSource
			}
			if credSource == config.SourceOAuth {
				out["token_status"] = rt.Config.TokenStatus()
				out["token_can_refresh"] = rt.Config.CanAutoRefresh()
				if !rt.Config.TokenExpiresAt.IsZero() {
					out["token_expires_at"] = rt.Config.TokenExpiresAt.Format(time.RFC3339)
				}
			}
			if conflict != nil {
				out["credential_conflict"] = conflict
			}
			if showScopes {
				if scopesKnown {
					out["scopes"] = scopes
				} else {
					out["scopes"] = nil
					out["scopes_available"] = false
				}
			}
			return rt.Out.Render(out)
		},
	}
	cmd.Flags().BoolVar(&showScopes, "scopes", false, "show the active credential's scopes (when determinable)")
	return cmd
}

func writeTokenStatus(rt *Runtime) {
	when := rt.Config.TokenExpiresAt.Local().Format("2006-01-02 15:04")
	canRefresh := rt.Config.CanAutoRefresh()
	switch rt.Config.TokenStatus() {
	case config.TokenValid:
		rt.Out.Field("Token", "valid until "+when)
	case config.TokenNearExpiry:
		if canRefresh {
			rt.Out.Field("Token", "expires "+when+" (refreshes automatically)")
		} else {
			rt.Out.Warn("Token expires " + when + " and can't auto-refresh; run `rc login`")
		}
	case config.TokenExpired:
		if canRefresh {
			rt.Out.Field("Token", "expired "+when+" (refreshes on next use)")
		} else {
			rt.Out.Warn("Token expired " + when + "; run `rc login` to re-authenticate")
		}
	default:
		rt.Out.Field("Token", "expiry unknown")
	}
}

// credentialScopes best-effort reports the scopes of the active credential.
//
// TODO(DX-940): read authoritative scopes once the API gains a token-introspection endpoint.
func credentialScopes(token string) (scopes []string, ok bool) {
	return jwtScopes(token)
}

// jwtScopes extracts scopes from a JWT's scope/scp/scopes claim; ok=false for non-JWTs.
func jwtScopes(token string) (scopes []string, ok bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, false
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, false
	}
	for _, key := range []string{"scope", "scp", "scopes"} {
		v, present := claims[key]
		if !present {
			continue
		}
		switch t := v.(type) {
		case string:
			if fields := strings.Fields(t); len(fields) > 0 {
				return fields, true
			}
		case []any:
			var out []string
			for _, item := range t {
				if s, isStr := item.(string); isStr && s != "" {
					out = append(out, s)
				}
			}
			if len(out) > 0 {
				return out, true
			}
		}
	}
	return nil, false
}

func loginWithAPIKeyInteractive(ctx context.Context, rt *Runtime) error {
	rt.Out.Info("Generate an API key at https://app.revenuecat.com/projects/-/api-keys")
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
	return loginWithAPIKeyOrigin(ctx, rt, key, config.AuthOriginAPIKey)
}

func loginWithAPIKeyOrigin(ctx context.Context, rt *Runtime, key, origin string) error {
	rt.Config.SetAPIKey(key)
	rt.Config.AuthSource = origin
	rt.Config.TokenType = ""
	rt.Config.AccessToken = ""
	rt.Config.RefreshToken = ""
	rt.Config.TokenExpiresAt = time.Time{}
	rt.Config.AccountEmail = ""
	rt.Config.AccountName = ""

	client, err := rt.API()
	if err != nil {
		return err
	}
	// Validate before clearing or saving: a bad key fails fast instead of a
	// false "Logged in", and a failed login won't announce a project clear.
	if _, err := client.Projects.List(ctx); err != nil {
		rt.Out.Hint("Check your key at https://app.revenuecat.com/projects/-/api-keys")
		return fmt.Errorf("that API key didn't work: %w", err)
	}
	clearProjectBinding(rt)
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
			fmt.Fprintf(w, "<html><body><p>Authorization failed — you may close this tab.</p></body></html>")
			select {
			case errCh <- fmt.Errorf("authorization denied: %s %s", e, desc):
			default:
			}
			return
		}
		if got := r.URL.Query().Get("state"); got != state {
			fmt.Fprintf(w, "<html><body><p>Invalid state — you may close this tab.</p></body></html>")
			select {
			case errCh <- fmt.Errorf("state mismatch (possible CSRF); please try logging in again"):
			default:
			}
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			fmt.Fprintf(w, "<html><body><p>Missing code — you may close this tab.</p></body></html>")
			select {
			case errCh <- fmt.Errorf("callback missing 'code' parameter"):
			default:
			}
			return
		}
		fmt.Fprintf(w, "<html><body><p>Authorization successful — you may close this tab.</p></body></html>")
		select {
		case codeCh <- code:
		default:
		}
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

	rt.Out.Info("Authorized — finishing sign-in…")

	tr, err := svc.ExchangeCode(ctx, code, redirectURI, verifier)
	if err != nil {
		return fmt.Errorf("token exchange: %w", err)
	}

	rt.Config.TokenType = "oauth"
	rt.Config.AccessToken = tr.AccessToken
	rt.Config.RefreshToken = tr.RefreshToken
	rt.Config.TokenExpiresAt = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	rt.Config.APIKey = ""
	rt.Config.AuthSource = config.AuthOriginOAuthLogin
	clearProjectBinding(rt)

	rt.client = nil
	client, err := rt.API()
	if err != nil {
		return err
	}
	return finishLogin(ctx, rt, client)
}

func signupWithOAuth(ctx context.Context, rt *Runtime, email, name, password string, marketingEmails, savePassword, generatedPassword bool) error {
	verifier, challenge, err := api.GeneratePKCE()
	if err != nil {
		return fmt.Errorf("generating PKCE: %w", err)
	}
	state, err := api.GenerateState()
	if err != nil {
		return fmt.Errorf("generating OAuth state: %w", err)
	}
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return fmt.Errorf("reserving local OAuth callback: %w", err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://localhost:%d/callback", port)

	svc := api.NewOAuthService(oauthBaseURL(), oauthClientID())
	rt.Out.Info("Creating your RevenueCat account…")
	if err := svc.ProvisionAccount(ctx, api.ProvisionAccountRequest{
		Email:                 email,
		Name:                  name,
		Password:              password,
		MarketingEmailEnabled: marketingEmails,
	}); err != nil {
		return fmt.Errorf("creating RevenueCat account: %w", err)
	}
	passwordSaved := false
	if savePassword {
		rt.Out.Info("Saving the website password in macOS Keychain…")
		if err := saveRevenueCatPasswordToKeychain(email, password); err != nil {
			rt.Out.Warn(fmt.Sprintf("Account created, but the password could not be saved to Keychain: %v", err))
		} else {
			passwordSaved = true
		}
	}

	rt.Out.Info("Starting a temporary secure login…")
	login, err := svc.Login(ctx, email, password)
	if err != nil {
		return signupAuthenticationError(err)
	}
	rt.Out.Info("Authorizing renewable CLI access…")
	code, err := svc.AuthorizeWithLoginToken(ctx, login.AuthenticationToken, redirectURI, challenge, state)
	if err != nil {
		return signupAuthenticationError(err)
	}
	rt.Out.Info("Exchanging the temporary session for OAuth tokens…")
	tokens, err := svc.ExchangeCode(ctx, code, redirectURI, verifier)
	if err != nil {
		return signupAuthenticationError(err)
	}
	_ = svc.LogoutLoginToken(ctx, login.AuthenticationToken)

	rt.Config.TokenType = "oauth"
	rt.Config.AccessToken = tokens.AccessToken
	rt.Config.RefreshToken = tokens.RefreshToken
	rt.Config.TokenExpiresAt = time.Now().Add(time.Duration(tokens.ExpiresIn) * time.Second)
	rt.Config.APIKey = ""
	rt.Config.AccountEmail = email
	rt.Config.AccountName = name
	rt.Config.AuthSource = config.AuthOriginOAuthLogin
	clearProjectBinding(rt)
	rt.client = nil

	rt.Out.Info("Saving OAuth credentials in the active CLI profile…")
	if err := config.Save(rt.Globals.Profile, rt.Config); err != nil {
		return err
	}
	profile := config.ProfileName(rt.Globals.Profile)
	rt.Out.Success(fmt.Sprintf("Account created and logged in (profile: %s)", profile))
	if generatedPassword && !passwordSaved {
		rt.Out.Warn("The generated password was not saved. Use password reset if you need dashboard access later.")
	}
	rt.Out.Info("Check your email to verify the account.")
	rt.Out.Info("Next, copy this into a new agent session:")
	rt.Out.Info(projectSkillTrigger)
	rt.Out.Hint("Install agent workflows:  rc skills install")
	rt.Out.Hint("Or start manually:  rc projects create --name \"My App\" --use")
	result := map[string]any{
		"account_created":             true,
		"authenticated":               true,
		"email":                       email,
		"email_verification_required": true,
		"method":                      "oauth",
		"password_saved_to_keychain":  passwordSaved,
		"password_mode":               "user_provided",
		"profile":                     profile,
		"next_steps": map[string]any{
			"agent":                  "Use the RevenueCat AI Toolkit to create and configure the project, apps, products, entitlements, and offerings.",
			"install_skills_command": "rc skills install",
			"manual_start_command":   "rc projects create --name \"My App\" --use",
		},
	}
	if generatedPassword {
		result["password_mode"] = "generated"
		if passwordSaved {
			result["dashboard_password_action"] = "saved_to_macos_keychain"
		} else {
			result["dashboard_password_action"] = "use_password_reset_if_needed"
		}
	} else if passwordSaved {
		result["dashboard_password_action"] = "saved_to_macos_keychain"
	} else {
		result["dashboard_password_action"] = "store_the_user_provided_password_safely"
	}
	if rt.Out.IsJSON() {
		return rt.Out.Render(result)
	}
	return nil
}

func validateSignupPassword(password string) error {
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	if len(password) > 72 {
		return fmt.Errorf("password must be at most 72 characters")
	}
	return nil
}

func signupAuthenticationError(err error) error {
	return fmt.Errorf("account was created, but OAuth setup failed: %w; use password reset at https://app.revenuecat.com if you need to recover the account", err)
}

// clearProjectBinding drops the profile's saved project when credentials
// change: the new account may not own it, and a stale binding surfaces as
// confusing authorization errors on every project-scoped command. An explicit
// --project-id / RC_PROJECT_ID passed alongside the login is kept.
func clearProjectBinding(rt *Runtime) {
	if rt.Globals.ProjectID != "" {
		return
	}
	if rt.Config.ProjectID != "" {
		rt.Out.Info("Cleared saved project " + rt.Config.ProjectID + ".")
		rt.Out.Hint("Pick one for this account:  rc projects use")
	}
	rt.Config.ProjectID = ""
}

func finishLogin(ctx context.Context, rt *Runtime, _ *api.Client) error {
	if err := config.Save(rt.Globals.Profile, rt.Config); err != nil {
		return err
	}
	rt.Out.Success(fmt.Sprintf("Logged in (profile: %s)", config.ProfileName(rt.Globals.Profile)))
	rt.Out.Hint("Set a default project:  rc projects use  (or pick one when prompted)")
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
		cmd = exec.Command("cmd", "/c", "start", "", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
