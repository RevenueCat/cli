package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

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

			// 1. Authenticate.
			if !rt.Globals.AssumeYes && rt.CanPrompt() {
				rt.Out.Notice(
					"Your Google sign-in stays local — RevenueCat never sees your Google",
					"credentials or tokens. The service-account key is created in memory.",
				)
				ok, err := tui.Confirm(rt.Globals.NoInput, "Sign in to Google now?")
				if err != nil {
					return err
				}
				if !ok {
					return errors.New("cancelled")
				}
			}
			rt.Out.Info("Opening your browser to sign in to Google…")
			creds, err := google.Authenticate(ctx, clientID, clientSecret, google.DefaultScopes, openBrowser)
			if err != nil {
				return err
			}
			if creds.Email != "" {
				rt.Out.Answer("Signed in", creds.Email)
			}
			result := googleSetupResult{Email: creds.Email}

			// 2. Choose the GCP project.
			if projectID == "" {
				projects, err := google.ListProjects(ctx, creds.TokenSource)
				if err != nil {
					return err
				}
				if len(projects) == 0 {
					return errors.New("no Google Cloud projects are accessible to this account")
				}
				choices := make([]Choice[string], 0, len(projects))
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
			}
			result.ProjectID = projectID

			// 3. Play developer account ID (no discovery API — must be supplied).
			if developerID == "" {
				if !rt.CanPrompt() {
					return errors.New("--developer-id is required under --no-input (Google exposes no API to discover it; find it in your Play Console URL)")
				}
				var raw string
				if err := tui.Form(rt.Globals.NoInput).
					Field(huh.NewInput().
						Title("Play Console URL or developer account ID").
						Description("Google has no API for this. Paste your Play Console URL (…/developers/NUMBER/…) or the numeric ID.").
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

			// 4. Package name.
			if packageName == "" {
				if !rt.CanPrompt() {
					return errors.New("--package is required under --no-input (couldn't detect it from this project)")
				}
				if err := tui.Form(rt.Globals.NoInput).
					Field(huh.NewInput().Title("Android package name").Placeholder("com.example.app").
						Value(&packageName).Validate(tui.Required("package name"))).
					Run(); err != nil {
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
			if err := google.EnableAPIs(ctx, creds.TokenSource, projectID); err != nil {
				return googleHint(rt, err)
			}
			result.EnabledAPIs = google.RequiredAPIs
			rt.Out.Success("Required APIs enabled")

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
	return cmd
}

// googleHint adds actionable guidance for the common org-policy failures.
func googleHint(rt *Runtime, err error) error {
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
