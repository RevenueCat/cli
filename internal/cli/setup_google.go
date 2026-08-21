package cli

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/google"
	"github.com/revenuecat/cli/internal/tui"
)

type googleSetupResult struct {
	Email               string   `json:"email,omitempty"`
	ProjectID           string   `json:"gcp_project_id,omitempty"`
	ServiceAccount      string   `json:"service_account,omitempty"`
	ServiceAccountNew   bool     `json:"service_account_created"`
	EnabledAPIs         []string `json:"enabled_apis,omitempty"`
	GrantedRoles        []string `json:"granted_roles,omitempty"`
	DeveloperID         string   `json:"play_developer_id,omitempty"`
	PackageName         string   `json:"package_name,omitempty"`
	PlayUserCreated     bool     `json:"play_user_created"`
	PlayGrantConfigured bool     `json:"play_grant_configured"`
	KeyPath             string   `json:"key_path,omitempty"`
	CredentialUploaded  bool     `json:"credential_uploaded"`
	RCAppID             string   `json:"rc_app_id,omitempty"`
}

const googleSetupInstructions = `This setup will:
  1. Sign you in to Google in your browser (nothing is sent to RevenueCat).
  2. Enable the required Google APIs on a Google Cloud project you choose.
  3. Create the RevenueCat service account and grant it the Cloud roles it needs.
  4. Create a service-account key (kept in memory, then written only where you ask).
  5. Add the service account to your Play developer account with package-scoped
     access to the app.

You must be a Google Cloud project owner/editor and a Play Console owner/admin.`

const googlePrivacyNotice = `Privacy:
  • Your Google sign-in happens locally in your browser. RevenueCat never
    receives your Google credentials or OAuth tokens.
  • The service-account key is created in memory. It is written to disk only at
    the path you choose, so you can upload it to RevenueCat.`

func newSetupGoogleCmd() *cobra.Command {
	var clientID, clientSecret string
	var projectID, developerID, packageName, keyOut string
	var noBrowser bool
	var keepOldKeys bool

	cmd := &cobra.Command{
		Use:   "google",
		Short: "Set up Google Play credentials for RevenueCat",
		Long: "Signs in to Google locally and bootstraps the service-account credential RevenueCat\n" +
			"needs for a Google Play app — enabling APIs, creating the service account and key,\n" +
			"and granting package-scoped Play Console access.\n\n" + googleSetupInstructions + "\n\n" + googlePrivacyNotice,
		Example: "  rc setup google\n  rc setup google --project my-app-prod --developer-id 5412345678901234567 --package com.example.app",
		Args:    cobra.MaximumNArgs(1),
		Annotations: map[string]string{
			"requires_human":        "true",
			"requires_human_reason": "Opens a browser for Google sign-in and manages Play Console access; a human must run it in a local interactive terminal.",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			clientID = valueOrEnv(clientID, "RC_GOOGLE_CLIENT_ID")
			clientSecret = valueOrEnv(clientSecret, "RC_GOOGLE_CLIENT_SECRET")
			if clientID == "" {
				clientID = google.DefaultClientID
				clientSecret = google.DefaultClientSecret
			}
			developerID = valueOrEnv(developerID, "RC_GOOGLE_DEVELOPER_ID")
			packageName = valueOrEnv(packageName, "RC_GOOGLE_PACKAGE")

			rt := RuntimeFrom(cmd.Context())
			ctx := cmd.Context()

			if clientID == "" {
				return google.ErrNoClientID
			}

			fl := tui.NewFlow(os.Stderr, !rt.CanPrompt())
			fl.Intro("RevenueCat · Google Play setup")

			if !rt.CanPrompt() && !rt.Globals.AssumeYes {
				return errors.New("rc setup google is interactive: run it in a terminal, or pass --yes with the app id, --project, and --developer-id")
			}

			// App — confirm the only one, or pick among several. Its package name
			// drives the Google setup.
			rc, err := rt.API()
			if err != nil {
				return err
			}
			rcProject, err := requireProject(rt)
			if err != nil {
				return err
			}
			chosenApp, err := resolvePlayApp(ctx, rt, rc, fl, rcProject, argAt(args, 0))
			if err != nil {
				return err
			}
			if packageName == "" {
				packageName = chosenApp.PlayStore.PackageName
			}

			// Sign in. Offer two equally-fine ways in (not a yes/no — "no" reads
			// as wrong when copying the link is a perfectly good choice).
			open := func(u string) error {
				if rt.Out.IsJSON() {
					// Info/LinkLine are suppressed in JSON mode, but the sign-in URL
					// is the only way to finish auth — always surface it on stderr.
					fmt.Fprintln(os.Stderr, "Sign in to Google to continue: "+u)
					return nil
				}
				method := "open"
				if noBrowser {
					method = "copy"
				} else if rt.CanPrompt() {
					choice, err := fl.Select("Sign in to Google",
						[]tui.Option{
							{Label: "Open in my browser", Value: "open"},
							{Label: "Copy the link — I'll open it myself", Value: "copy"},
						},
						"Your sign-in stays local — RevenueCat never sees your Google credentials.",
						"Use the account with access to this app's Play Console and Cloud project.",
					)
					if err != nil {
						return err
					}
					method = choice
				}
				if method == "open" && openBrowser(u) == nil {
					fl.Say("Opened your browser. Sign in with the right account.")
				} else {
					if clipboard.WriteAll(u) == nil {
						fl.Say("Copied the link to your clipboard — paste it into your browser:")
					} else {
						fl.Say("Sign in with this link:")
					}
					fl.URL(u)
				}
				fl.Say("Waiting for sign-in…")
				return nil
			}
			creds, err := google.Authenticate(ctx, clientID, clientSecret, google.DefaultScopes, open)
			if err != nil {
				return err
			}
			if creds.Email != "" {
				fl.Receipt("Signed in", creds.Email)
			}
			result := googleSetupResult{Email: creds.Email, RCAppID: chosenApp.ID}

			// Google Cloud project — or create a new one.
			if projectID == "" {
				projects, err := google.ListProjects(ctx, creds.TokenSource)
				if err != nil {
					return err
				}
				opts := []tui.Option{{Label: "➕ Create a new project", Value: createNewProjectSentinel}}
				for _, p := range projects {
					label := p.ID
					if p.DisplayName != "" {
						label = fmt.Sprintf("%s (%s)", p.DisplayName, p.ID)
					}
					opts = append(opts, tui.Option{Label: label, Value: p.ID})
				}
				projectID, err = fl.Select("Google Cloud project", opts)
				if err != nil {
					return err
				}
				if projectID == createNewProjectSentinel {
					projectID, err = createGoogleProject(ctx, rt, creds.TokenSource)
					if err != nil {
						return err
					}
				}
			} else {
				fl.Step("Google Cloud project")
				fl.Receipt("Project", projectID)
			}
			result.ProjectID = projectID

			// Play developer account (no discovery API — must be supplied).
			if developerID == "" {
				if !rt.CanPrompt() {
					return errors.New("--developer-id is required under --no-input (Google exposes no API to discover it; find it in your Play Console URL)")
				}
				desc := []string{
					"Google has no API for this — copy it from your Play Console URL.",
					rt.Out.LinkText("Open Play Console ↗", "https://play.google.com/console/"),
					"Your ID is the number after /developers/ in the URL.",
				}
				raw, err := fl.Input("Play developer account", "…/developers/1234567890123456789/…", tui.Required("developer account ID"), desc...)
				if err != nil {
					return err
				}
				developerID, err = google.ParseDeveloperID(raw)
				if err != nil {
					return err
				}
			} else {
				fl.Step("Play developer account")
				developerID, err = google.ParseDeveloperID(developerID)
				if err != nil {
					return err
				}
				fl.Receipt("Developer account", developerID)
			}
			result.DeveloperID = developerID

			// 4. Package name comes from the chosen RevenueCat app. Only prompt if
			// that app has no package set yet.
			if packageName == "" {
				if !rt.CanPrompt() {
					return fmt.Errorf("app %s has no package name set — pass --package", chosenApp.ID)
				}
				packageName, err = promptPackage(rt)
				if err != nil {
					return err
				}
			}
			result.PackageName = packageName

			saEmail := google.ServiceAccountEmail(projectID)
			if keyOut == "" {
				keyOut = "revenuecat-play-key.json"
			}

			// Review + single consent.
			fl.Step("Review")
			fl.Say(fmt.Sprintf("Enable %d Google APIs", len(google.RequiredAPIs)))
			fl.Say("Create " + saEmail)
			fl.Say("Grant " + strings.Join(google.ProjectRoles, " + "))
			fl.Say(keyPlanStep(keepOldKeys))
			fl.Say("Add to Google Play · grant access to " + packageName)
			fl.Say("Upload the credential to RevenueCat")
			if !rt.Globals.AssumeYes {
				ok, err := fl.Confirm("Continue?", true)
				if err != nil {
					return err
				}
				if !ok {
					return errors.New("cancelled")
				}
			}

			// Setting up. Enable APIs first — it can pause for the Android Publisher
			// terms of service — then the rest fills in on the rail ledger.
			fl.Step("Setting up")
			if err := enableAPIsWithToS(ctx, rt, creds.TokenSource, projectID, noBrowser, creds.Email); err != nil {
				return err
			}
			result.EnabledAPIs = google.RequiredAPIs
			result.ServiceAccount = saEmail
			result.GrantedRoles = google.ProjectRoles
			result.PackageName = packageName

			led := fl.Ledger(
				"Create the RevenueCat service account",
				"Grant Google Cloud roles",
				"Create the service-account key",
				"Add the service account to Google Play",
				"Upload the credential to RevenueCat",
			)
			led.Start()

			created, _, err := google.EnsureServiceAccount(ctx, creds.TokenSource, projectID)
			if err != nil {
				led.Fail(0, "failed")
				led.Stop()
				return googleHint(rt, err)
			}
			result.ServiceAccountNew = created
			if created {
				led.Done(0, "created")
			} else {
				led.Done(0, "already existed")
			}

			led.Running(1)
			added, err := google.GrantProjectRoles(ctx, creds.TokenSource, projectID, saEmail)
			if err != nil {
				led.Fail(1, "failed")
				led.Stop()
				return googleHint(rt, err)
			}
			if len(added) == 0 {
				led.Done(1, "already granted")
			} else {
				led.Done(1, "")
			}

			led.Running(2)
			keyData, keyName, err := google.CreateKey(ctx, creds.TokenSource, projectID, saEmail)
			if err != nil {
				led.Fail(2, "failed")
				led.Stop()
				return googleHint(rt, err)
			}
			led.Done(2, "in memory")

			led.Running(3)
			play, err := google.AddServiceAccountToPlay(ctx, creds.TokenSource, developerID, saEmail, packageName, projectID)
			if err != nil {
				led.Fail(3, "failed")
				led.Stop()
				return googleHint(rt, err)
			}
			result.PlayUserCreated = play.UserCreated
			result.PlayGrantConfigured = true
			led.Done(3, packageName)

			// Upload the credential to RevenueCat via API v2. If that fails, fall
			// back to writing the key so the human can upload it in the dashboard —
			// but surface the real error so a transient/auth/validation failure
			// isn't silently mistaken for "endpoint not available". This runs
			// before pruning old keys: Google returns key material only once.
			led.Running(4)
			credJSON := string(keyData)
			_, upErr := rc.Apps.Update(ctx, rcProject, chosenApp.ID, api.AppUpdate{
				PlayStore: &api.PlayStoreAppConfig{PlayServiceAccountCredentialsJSON: &credJSON},
			})
			if upErr == nil {
				result.CredentialUploaded = true
				led.Done(4, "uploaded to RevenueCat")
			} else if writeErr := os.WriteFile(keyOut, keyData, 0o600); writeErr == nil {
				result.KeyPath = keyOut
				led.Done(4, "saved to "+keyOut+" (upload it in the dashboard)")
				fl.Warn("RevenueCat didn't accept the upload: " + upErr.Error())
			} else {
				led.Fail(4, "failed")
				led.Stop()
				return fmt.Errorf("upload credential (%v) and write key to %s (%v)", upErr, keyOut, writeErr)
			}
			led.Stop()

			if !keepOldKeys {
				if pruned, perr := google.PruneUserKeys(ctx, creds.TokenSource, projectID, saEmail, keyName); perr != nil {
					fl.Warn("Couldn't remove older keys on the service account: " + perr.Error())
				} else if pruned > 0 {
					fl.Say(fmt.Sprintf("Removed %d older key(s) (pass --keep-old-keys to keep them).", pruned))
				}
			}

			if rt.Out.IsJSON() {
				return rt.Out.Render(result)
			}
			uploadURL := "https://app.revenuecat.com/projects/" + rcProject + "/apps/" + chosenApp.ID
			if result.CredentialUploaded {
				fl.Outro("Google Play connected 🎉",
					rt.Out.LinkText("Open your app in RevenueCat ↗", uploadURL),
				)
			} else {
				fl.Outro("Almost there",
					"Upload the credential to finish:",
					rt.Out.LinkText("Upload "+keyOut+" ↗", uploadURL),
				)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&clientID, "client-id", "", "Google OAuth Desktop client ID (env: RC_GOOGLE_CLIENT_ID)")
	cmd.Flags().StringVar(&clientSecret, "client-secret", "", "Google OAuth Desktop client secret (env: RC_GOOGLE_CLIENT_SECRET)")
	cmd.Flags().StringVar(&projectID, "project", "", "Google Cloud project ID")
	cmd.Flags().StringVar(&developerID, "developer-id", "", "Play developer account ID or Console URL (env: RC_GOOGLE_DEVELOPER_ID)")
	cmd.Flags().StringVar(&packageName, "package", "", "Android package name (env: RC_GOOGLE_PACKAGE)")
	cmd.Flags().StringVar(&keyOut, "key-out", "revenuecat-play-key.json", "path to write the service-account key for upload to RevenueCat")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "don't open a browser automatically; print the sign-in URL to open manually")
	cmd.Flags().BoolVar(&keepOldKeys, "keep-old-keys", false, "keep existing keys on the RevenueCat service account instead of replacing them (each run otherwise removes old keys before creating a fresh one)")
	return cmd
}

// enableAPIsWithToS enables the required APIs, and when Google reports the
// Android Publisher terms of service aren't accepted yet, it walks the user
// through accepting them (opens the page, waits) and retries — rather than
// failing the whole run and discarding all the answers already given.
func enableAPIsWithToS(ctx context.Context, rt *Runtime, ts oauth2.TokenSource, projectID string, noBrowser bool, email string) error {
	for attempt := 0; ; attempt++ {
		err := google.EnableAPIs(ctx, ts, projectID)
		if err == nil {
			rt.Out.Success("Required APIs enabled")
			return nil
		}
		var tos *google.TosError
		if !errors.As(err, &tos) || !rt.CanPrompt() || attempt >= 4 {
			return googleHint(rt, err)
		}
		acceptURL := addParam(addParam(tos.URL, "authuser", email), "project", projectID)
		rt.Out.Blank()
		if attempt == 0 {
			rt.Out.Warn("Google needs you to accept the Android Publisher API terms of service — a one-time step.")
		} else {
			rt.Out.Warn("Still not accepted. This must be accepted by the SAME Google account you signed in with — not whatever account your default browser happens to use.")
		}
		if email != "" {
			rt.Out.Info("Accept it while signed in as " + email + ":")
		} else {
			rt.Out.Info("Accept it while signed in as the account you used to sign in here:")
		}
		rt.Out.LinkLine(acceptURL)
		// Open the browser only on the first try — reopening every retry is
		// noise, and if it landed on the wrong account the link above (with
		// authuser) plus the explicit account name is the fix, not another tab.
		if attempt == 0 && !noBrowser {
			_ = openBrowser(acceptURL)
		}
		ok, err := tui.ConfirmDefault(rt.Globals.NoInput, "Press Enter once you've accepted the terms to continue", true)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("cancelled — accept the Android Publisher terms and run rc setup google again")
		}
		rt.Out.Info("Retrying…")
	}
}

// addParam appends a query parameter to a URL (empty values are skipped). Used
// to steer the ToS page to the right Google account (authuser) and project —
// acceptance is scoped to the project, so both matter.
func addParam(u, key, value string) string {
	if value == "" {
		return u
	}
	sep := "?"
	if strings.Contains(u, "?") {
		sep = "&"
	}
	return u + sep + key + "=" + url.QueryEscape(value)
}

// keyPlanStep names the key step in the plan so consent reflects whether old
// keys will be replaced.
func keyPlanStep(keepOldKeys bool) string {
	if keepOldKeys {
		return "Create a service-account key (in memory)"
	}
	return "Replace any old keys and create a fresh service-account key (in memory)"
}

// resolvePlayApp picks the RevenueCat Play app to configure: use the given id
// (confirmed), confirm the only one, or pick among several. Returns the app so
// its package name can drive the Google setup.
func resolvePlayApp(ctx context.Context, rt *Runtime, rc *api.Client, fl *tui.Flow, projectID, appIDArg string) (*api.App, error) {
	if appIDArg != "" {
		app, err := rc.Apps.Get(ctx, projectID, appIDArg)
		if err != nil {
			return nil, err
		}
		if app.Type != "play_store" || app.PlayStore == nil {
			return nil, fmt.Errorf("app %s is not a Google Play app", appIDArg)
		}
		if err := confirmPlayApp(rt, fl, app); err != nil {
			return nil, err
		}
		return app, nil
	}
	page, err := rc.Apps.List(ctx, projectID)
	if err != nil {
		return nil, err
	}
	var play []api.App
	for _, a := range page.Items {
		if a.Type == "play_store" && a.PlayStore != nil {
			play = append(play, a)
		}
	}
	switch len(play) {
	case 0:
		return nil, errors.New("no Google Play apps in this project — create one first (rc apps create)")
	case 1:
		if err := confirmPlayApp(rt, fl, &play[0]); err != nil {
			return nil, err
		}
		return &play[0], nil
	default:
		if !rt.CanPrompt() {
			return nil, errors.New("multiple Google Play apps in this project — pass the app id: rc setup google <app-id>")
		}
		opts := make([]tui.Option, len(play))
		for i := range play {
			opts[i] = tui.Option{Label: playAppLabel(&play[i]), Value: play[i].ID}
		}
		id, err := fl.Select("App", opts)
		if err != nil {
			return nil, err
		}
		for i := range play {
			if play[i].ID == id {
				return &play[i], nil
			}
		}
		return nil, fmt.Errorf("app %s not found", id)
	}
}

// confirmPlayApp asks the human to confirm the app before doing anything to it.
func confirmPlayApp(rt *Runtime, fl *tui.Flow, app *api.App) error {
	if rt.Globals.AssumeYes || !rt.CanPrompt() {
		return nil
	}
	ok, err := fl.Confirm("Set up Google Play for "+playAppLabel(app)+"?", true)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("cancelled")
	}
	return nil
}

func playAppLabel(app *api.App) string {
	if app.PlayStore != nil && app.PlayStore.PackageName != "" {
		return app.Name + " (" + app.PlayStore.PackageName + ")"
	}
	return app.Name + " (" + app.ID + ")"
}

func promptPackage(rt *Runtime) (string, error) {
	var pkg string
	if err := tui.Form(rt.Globals.NoInput).
		Field(huh.NewInput().Title("Android package name").Placeholder("com.example.app").
			Value(&pkg).Validate(tui.Required("package name"))).
		Run(); err != nil {
		return "", err
	}
	return pkg, nil
}

// createNewProjectSentinel is the picker value that means "make a new project"
// rather than selecting an existing one.
const createNewProjectSentinel = "\x00create-new-project"

// createGoogleProject prompts for a name, derives a valid project ID (editable),
// and creates the project, returning its ID.
func createGoogleProject(ctx context.Context, rt *Runtime, ts oauth2.TokenSource) (string, error) {
	var name string
	if err := tui.Form(rt.Globals.NoInput).
		Field(huh.NewInput().Title("New project name").Placeholder("RevenueCat Play").
			Value(&name).Validate(tui.Required("project name"))).
		Run(); err != nil {
		return "", err
	}
	projectID := google.ProjectIDFromName(name)
	if err := tui.Form(rt.Globals.NoInput).
		Field(huh.NewInput().Title("Project ID").
			Description("Globally unique, 6–30 chars, lowercase letters/digits/hyphens. Edit if you like.").
			Value(&projectID).Validate(tui.Required("project ID"))).
		Run(); err != nil {
		return "", err
	}
	rt.Out.Info("Creating project " + projectID + " (this can take a moment)…")
	if err := google.CreateProject(ctx, ts, projectID, name); err != nil {
		return "", err
	}
	rt.Out.Success("Created project " + projectID)
	return projectID, nil
}

// googleHint adds actionable guidance for the common one-time-setup failures.
func googleHint(rt *Runtime, err error) error {
	var tos *google.TosError
	if errors.As(err, &tos) {
		rt.Out.Warn("Google needs you to accept the Android Publisher API terms of service first (a one-time step for this Google account).")
		rt.Out.Info("Accept it here, then re-run rc setup google:")
		rt.Out.LinkLine(tos.URL)
		return err
	}
	var op *google.OrgPolicyError
	if errors.As(err, &op) {
		switch op.Constraint {
		case "iam.disableServiceAccountKeyCreation":
			rt.Out.Hint("Your Google Cloud organization disables service-account key creation. Ask an org admin to allow it for this project, then re-run rc setup google.")
		case "iam.disableServiceAccountCreation":
			rt.Out.Hint("Your Google Cloud organization disables service-account creation. Ask an org admin to allow it for this project, then re-run rc setup google.")
		}
	}
	return err
}
