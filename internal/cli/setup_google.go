package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"

	"github.com/revenuecat/cli/internal/google"
	"github.com/revenuecat/cli/internal/output"
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

			rt.Out.Title("Google Play configuration")
			rt.Out.Lead("Signs in to Google locally and creates the credential RevenueCat needs. Nothing changes without your OK.")

			if !rt.CanPrompt() && !rt.Globals.AssumeYes {
				return errors.New("rc setup google is interactive: run it in a terminal, or pass --yes with --project, --developer-id, and --package")
			}

			// Default the package name from the local project if not supplied.
			if packageName == "" {
				if wd, err := os.Getwd(); err == nil {
					if detected := google.DetectPackageName(wd); detected != "" {
						packageName = detected
						rt.Out.Info("Detected package name from this project: " + detected)
					}
				}
			}

			// 1. Authenticate. Print the URL first, then let the user open it
			// (Stripe-style) so they can pick the browser / Google account that
			// has their Play + Cloud access.
			if !rt.Globals.AssumeYes && rt.CanPrompt() {
				rt.Out.Notice(
					"Your Google sign-in stays local — RevenueCat never sees your Google",
					"credentials or tokens. The service-account key is created in memory.",
				)
			}
			open := func(u string) error {
				rt.Out.Blank()
				rt.Out.Info("Sign in to Google to continue — use the browser/account that has your Play + Cloud access:")
				rt.Out.LinkLine(u)
				rt.Out.Blank()
				switch {
				case noBrowser:
					rt.Out.Info("Waiting for you to finish sign-in in your browser…")
					return nil
				case rt.Globals.AssumeYes || !rt.CanPrompt():
					// non-interactive / --yes: open without pausing
				default:
					openIt, err := tui.ConfirmDefault(rt.Globals.NoInput, "Press Enter to open it in your browser (or choose No to open it yourself)", true)
					if err != nil {
						return err
					}
					if !openIt {
						rt.Out.Info("Open the URL above to finish. Waiting…")
						return nil
					}
				}
				if err := openBrowser(u); err != nil {
					rt.Out.Warn("Couldn't open a browser automatically — open the URL above. Waiting…")
				} else {
					rt.Out.Info("Opened your browser. Waiting for sign-in to complete…")
				}
				return nil
			}
			creds, err := google.Authenticate(ctx, clientID, clientSecret, google.DefaultScopes, open)
			if err != nil {
				return err
			}
			if creds.Email != "" {
				rt.Out.Answer("Signed in", creds.Email)
			}
			result := googleSetupResult{Email: creds.Email}

			// 2. Choose the GCP project — or create a new one.
			if projectID == "" {
				projects, err := google.ListProjects(ctx, creds.TokenSource)
				if err != nil {
					return err
				}
				choices := []Choice[string]{{Value: createNewProjectSentinel, Label: "➕ Create a new project", Flag: "--project"}}
				for _, p := range projects {
					label := p.ID
					if p.DisplayName != "" {
						label = fmt.Sprintf("%s (%s)", p.DisplayName, p.ID)
					}
					choices = append(choices, Choice[string]{Value: p.ID, Label: label, Flag: "--project"})
				}
				projectID, err = decide(rt, "Google Cloud project", nil, choices)
				if err != nil {
					return err
				}
				if projectID == createNewProjectSentinel {
					projectID, err = createGoogleProject(ctx, rt, creds.TokenSource)
					if err != nil {
						return err
					}
				}
			}
			result.ProjectID = projectID

			// 3. Play developer account ID (no discovery API — must be supplied).
			if developerID == "" {
				if !rt.CanPrompt() {
					return errors.New("--developer-id is required under --no-input (Google exposes no API to discover it; find it in your Play Console URL)")
				}
				rt.Out.Notice(
					"Google has no API for your Play developer account ID, so paste it here.",
				)
				rt.Out.Info("1. Open your Play Console:")
				rt.Out.LinkLine("https://play.google.com/console/")
				rt.Out.Info("2. Copy the page URL — it looks like:")
				rt.Out.Info("      …/developers/1234567890123456789/app-list   (that number is your ID)")
				rt.Out.Info("3. Paste the whole URL below — I'll pull out the ID (or paste just the number).")
				rt.Out.Info("Can't find it?")
				rt.Out.LinkLine("https://support.google.com/googleplay/android-developer/answer/13634081")
				var raw string
				if err := tui.Form(rt.Globals.NoInput).
					Field(huh.NewInput().
						Title("Paste your Play Console URL (or the 19-digit ID)").
						Placeholder("https://play.google.com/console/u/0/developers/1234567890123456789/app-list").
						Value(&raw).Validate(tui.Required("developer account ID"))).
					Run(); err != nil {
					return err
				}
				developerID, err = google.ParseDeveloperID(raw)
				if err != nil {
					return err
				}
			} else {
				developerID, err = google.ParseDeveloperID(developerID)
				if err != nil {
					return err
				}
			}
			result.DeveloperID = developerID

			// 4. Package name — local detection already ran; otherwise offer a
			// picker of the user's Play apps, falling back to manual entry.
			if packageName == "" {
				if !rt.CanPrompt() {
					return errors.New("--package is required under --no-input (couldn't detect it from this project)")
				}
				packageName, err = choosePackage(ctx, rt, creds.TokenSource)
				if err != nil {
					return err
				}
			}
			result.PackageName = packageName

			saEmail := google.ServiceAccountEmail(projectID)
			if keyOut == "" {
				keyOut = "revenuecat-play-key.json"
			}

			// Plan + single consent.
			rt.Out.Plan([]string{
				"Enable " + fmt.Sprintf("%d", len(google.RequiredAPIs)) + " Google APIs on " + projectID,
				"Create service account " + saEmail,
				"Grant " + strings.Join(google.ProjectRoles, " + "),
				"Create a service-account key (in memory)",
				"Add the service account to Play and grant access to " + packageName,
				"Write the key to " + keyOut + " for upload to RevenueCat",
			})
			if err := confirmOrAbort(rt, "Continue?"); err != nil {
				return err
			}

			// 5. Execute.
			rt.Out.Info("Enabling Google APIs…")
			if err := enableAPIsWithToS(ctx, rt, creds.TokenSource, projectID, noBrowser); err != nil {
				return err
			}
			result.EnabledAPIs = google.RequiredAPIs

			rt.Out.Info("Creating the RevenueCat service account…")
			created, email, err := google.EnsureServiceAccount(ctx, creds.TokenSource, projectID)
			if err != nil {
				return googleHint(rt, err)
			}
			saEmail = email
			result.ServiceAccount = saEmail
			result.ServiceAccountNew = created
			if created {
				rt.Out.Success("Created service account " + saEmail)
			} else {
				rt.Out.Success("Found existing service account " + saEmail)
			}

			rt.Out.Info("Granting Google Cloud roles…")
			added, err := google.GrantProjectRoles(ctx, creds.TokenSource, projectID, saEmail)
			if err != nil {
				return googleHint(rt, err)
			}
			result.GrantedRoles = google.ProjectRoles
			if len(added) == 0 {
				rt.Out.Success("Cloud roles already granted")
			} else {
				rt.Out.Success("Granted " + strings.Join(added, ", "))
			}

			rt.Out.Info("Creating the service-account key…")
			keyData, err := google.CreateKey(ctx, creds.TokenSource, projectID, saEmail)
			if err != nil {
				return googleHint(rt, err)
			}
			rt.Out.Success("Created service-account key (in memory)")

			rt.Out.Info("Adding the service account to Google Play…")
			play, err := google.AddServiceAccountToPlay(ctx, creds.TokenSource, developerID, saEmail, packageName)
			if err != nil {
				return googleHint(rt, err)
			}
			result.PlayUserCreated = play.UserCreated
			result.PlayGrantConfigured = true
			switch {
			case play.UserCreated:
				rt.Out.Success("Added the service account to Play and granted access to " + packageName)
			case play.GrantUpdated:
				rt.Out.Success("Updated Play permissions for " + packageName)
			case play.GrantCreated:
				rt.Out.Success("Granted Play access to " + packageName)
			default:
				rt.Out.Success("Play access already configured for " + packageName)
			}

			// 6. RevenueCat upload is not yet supported by API v2 (see DX-985):
			// write the key so the human can upload it, until the API field exists.
			if err := os.WriteFile(keyOut, keyData, 0o600); err != nil {
				return fmt.Errorf("write key to %s: %w", keyOut, err)
			}
			result.KeyPath = keyOut

			rt.Out.Success("Google Play setup complete")
			if err := rt.Out.RenderCard(output.Card{
				Title:    "Google Play — " + packageName,
				Subtitle: creds.Email,
				Sections: []output.CardSection{{
					Heading: "Configured",
					Lines: []output.CardLine{
						{Key: "GCP project", Value: projectID},
						{Key: "Service account", Value: saEmail},
						{Key: "Cloud roles", Value: strings.Join(google.ProjectRoles, ", ")},
						{Key: "Play access", Value: packageName + " (view + financial + orders)"},
						{Key: "Credential key", Value: keyOut},
					},
				}},
				Raw: result,
			}); err != nil {
				return err
			}
			rt.Out.Hint("RevenueCat can't yet accept the Play credential over the API (DX-985). For now, upload " + keyOut + " in the dashboard: Project settings → your Play app → Service Account credentials JSON.")
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
	return cmd
}

// enableAPIsWithToS enables the required APIs, and when Google reports the
// Android Publisher terms of service aren't accepted yet, it walks the user
// through accepting them (opens the page, waits) and retries — rather than
// failing the whole run and discarding all the answers already given.
func enableAPIsWithToS(ctx context.Context, rt *Runtime, ts oauth2.TokenSource, projectID string, noBrowser bool) error {
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
		rt.Out.Blank()
		rt.Out.Warn("Google needs you to accept the Android Publisher API terms of service — a one-time step for this Google account.")
		rt.Out.Info("Accept it on this page (opening it now), then come back here:")
		rt.Out.LinkLine(tos.URL)
		if !noBrowser {
			_ = openBrowser(tos.URL)
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

// choosePackage lets the user pick from their Play apps (via the Reporting
// API) or enter a package manually. Falls back to manual entry if listing
// isn't available (e.g. the Reporting API isn't enabled or returns nothing).
func choosePackage(ctx context.Context, rt *Runtime, ts oauth2.TokenSource) (string, error) {
	const manual = "\x00manual-package"
	apps, err := google.ListPlayApps(ctx, ts)
	if err != nil {
		rt.Out.Warn("Couldn't list your Play apps automatically: " + err.Error())
		rt.Out.Hint("Enable the Play Developer Reporting API on your OAuth client's project to get a picker, or just enter the package below.")
		return promptPackage(rt)
	}
	if len(apps) == 0 {
		rt.Out.Info("No Play apps found for this account yet — enter the package name.")
		return promptPackage(rt)
	}
	choices := make([]Choice[string], 0, len(apps)+1)
	for _, a := range apps {
		label := a.PackageName
		if a.DisplayName != "" {
			label = fmt.Sprintf("%s (%s)", a.DisplayName, a.PackageName)
		}
		choices = append(choices, Choice[string]{Value: a.PackageName, Label: label, Flag: "--package"})
	}
	choices = append(choices, Choice[string]{Value: manual, Label: "✎ Enter a package name manually", Flag: "--package"})
	pkg, err := decide(rt, "Google Play app", nil, choices)
	if err != nil {
		return "", err
	}
	if pkg == manual {
		return promptPackage(rt)
	}
	return pkg, nil
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
