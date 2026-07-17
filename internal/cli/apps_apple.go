package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/appleconnect"
	"github.com/revenuecat/cli/internal/output"
	"github.com/revenuecat/cli/internal/tui"
)

type appleConfigurationResult struct {
	AppID                    string   `json:"app_id"`
	ProviderID               int64    `json:"provider_id"`
	ProviderName             string   `json:"provider_name"`
	Mode                     string   `json:"mode"`
	AlreadyConfigured        bool     `json:"already_configured,omitempty"`
	WouldCreate              []string `json:"would_create,omitempty"`
	InAppPurchaseKeyAccess   bool     `json:"in_app_purchase_key_access,omitempty"`
	AppStoreConnectKeyAccess bool     `json:"app_store_connect_key_access,omitempty"`
	InAppPurchaseKeyID       string   `json:"in_app_purchase_key_id,omitempty"`
	AppStoreConnectAPIKeyID  string   `json:"app_store_connect_api_key_id,omitempty"`
	VendorNumberConfigured   bool     `json:"vendor_number_configured"`
}

type appleConnectClient interface {
	Login(context.Context, string, string) (*appleconnect.Session, error)
	PrepareTwoFactor(context.Context, *appleconnect.Session, bool, string) (*appleconnect.Challenge, error)
	CompleteTwoFactor(context.Context, *appleconnect.Session, string) error
	SelectProvider(context.Context, *appleconnect.Session, int64) error
	CheckKeyAccess(context.Context, *appleconnect.Session, appleconnect.KeyKind) error
	CreateInAppPurchaseKey(context.Context, *appleconnect.Session, string) (*appleconnect.Key, error)
	CreateAppStoreConnectKey(context.Context, *appleconnect.Session, string) (*appleconnect.Key, error)
	FetchVendorNumber(context.Context, *appleconnect.Session) (string, error)
	AppExists(context.Context, *appleconnect.Session, string) (bool, error)
	RegisterBundleID(context.Context, *appleconnect.Session, string, string) error
	CreateApp(context.Context, *appleconnect.Session, string, string, string) error
}

type appleConnectFactory func() (appleConnectClient, error)

// ensureAppStoreAppRecord checks that the RevenueCat app's bundle ID has an
// App Store Connect app record and offers to create it (Developer Portal
// bundle ID + ASC app, like fastlane produce) when it doesn't. A missing
// record never blocks key setup — keys are account-level — so declining or
// non-interactive runs just get a warning.
func ensureAppStoreAppRecord(ctx context.Context, rt *Runtime, apple appleConnectClient, session *appleconnect.Session, app *api.App) error {
	bundleID := app.AppStore.BundleID
	if bundleID == "" {
		return nil
	}
	rt.Out.Info("Checking App Store Connect for bundle ID " + bundleID + "…")
	exists, err := apple.AppExists(ctx, session, bundleID)
	if err != nil {
		rt.Out.Warn("Could not verify the App Store Connect app record: " + err.Error())
		return nil
	}
	if exists {
		rt.Out.Info("App Store Connect app record found.")
		return nil
	}
	rt.Out.Warn("No App Store Connect app exists for " + bundleID + " — products and TestFlight need one.")
	if rt.Globals.NoInput || !tui.IsInteractive() {
		rt.Out.Warn("Create it in App Store Connect, or re-run rc apps apple setup interactively to create it from here.")
		return nil
	}
	create, err := tui.ConfirmDefault(rt.Globals.NoInput,
		"Create it now? (registers the bundle ID in the Developer Portal and creates the App Store Connect app)", true)
	if err != nil {
		return err
	}
	if !create {
		rt.Out.Info("Skipped — create the app in App Store Connect before configuring products.")
		return nil
	}
	name := strings.TrimSpace(strings.TrimSuffix(app.Name, "(App Store)"))
	sku := bundleID
	if err := tui.Form(rt.Globals.NoInput).
		Field(huh.NewInput().Title("App name (shown on the App Store)").Value(&name).Validate(tui.Required("app name"))).
		Field(huh.NewInput().Title("SKU (internal ID, not visible on the App Store)").Value(&sku).Validate(tui.Required("SKU"))).
		Run(); err != nil {
		return err
	}
	rt.Out.Info("Registering bundle ID " + bundleID + " in the Developer Portal…")
	if err := apple.RegisterBundleID(ctx, session, bundleID, name); err != nil {
		return err
	}
	rt.Out.Info("Creating the App Store Connect app…")
	if err := apple.CreateApp(ctx, session, name, bundleID, sku); err != nil {
		return err
	}
	rt.Out.Success("Created App Store Connect app " + name + " (" + bundleID + ")")
	return nil
}

func appleConfiguredLabel(configured bool) string {
	if configured {
		return "configured"
	}
	return "not configured"
}

// decideAppleKey resolves whether to create a key, preferring an explicit
// answer over a silent guess: interactive setups are asked per key (create
// missing ones, or replace existing ones); non-interactive runs create what
// is missing and only replace with --force. --skip-* always wins.
func decideAppleKey(rt *Runtime, name string, configured, skip, force, mayPrompt bool) (bool, error) {
	switch {
	case skip:
		return false, nil
	case force:
		return true, nil
	case !mayPrompt || rt.Globals.NoInput || rt.Globals.AssumeYes || !tui.IsInteractive():
		return !configured, nil
	case configured:
		return tui.ConfirmDefault(false, "Replace the existing "+name+"? (a new key is created in App Store Connect and uploaded)", false)
	default:
		return tui.ConfirmDefault(false, "Create and upload a new "+name+"?", true)
	}
}

func newAppleConnectClient() (appleConnectClient, error) {
	return appleconnect.New(appleconnect.Options{})
}

const appleSetupInstructions = `This setup will:
  1. Show the app's current Apple configuration and ask per key whether to
     create or replace anything (nothing happens without consent).
  2. Sign in directly to Apple with your Apple Account, with trusted-device
     or SMS verification when Apple requires it.
  3. Offer to create the App Store Connect app record when the bundle ID has
     none (Developer Portal registration + ASC app).
  4. Create the Apple keys you confirmed, download each private key once, and
     upload it directly to RevenueCat.
  5. Fetch your sales-report vendor number from Apple and confirm before
     setting it on the RevenueCat app.

Before continuing, have a trusted Apple device or phone available and use an
Apple Account with permission to manage App Store Connect integration keys.`

const applePrivacyNotice = `Privacy:
  • Your Apple Account credentials are sent directly to Apple. They are never
    sent to RevenueCat or stored by rc.
  • The Apple session exists only in memory for the duration of this command.
  • Newly created private keys are uploaded directly to RevenueCat. They are
    never saved locally or printed.`

const appleCheckPrivacyNotice = `Privacy:
  • Your Apple Account credentials are sent directly to Apple. They are never
    sent to RevenueCat or stored by rc.
  • The Apple session exists only in memory for the duration of this command.
  • This check creates no Apple keys and makes no changes in RevenueCat.`

const appleCheckInstructions = `This check will:
  1. Sign in directly to Apple and complete two-factor authentication.
  2. Select the App Store Connect team when the account has more than one.
  3. Make read-only requests to both Apple key-management endpoints.
  4. Report which keys a real run would create.

No Apple keys will be created and no RevenueCat app will be changed.`

func newAppsAppleCmd() *cobra.Command {
	return newAppsAppleCmdWithFactory(newAppleConnectClient)
}

func newAppsAppleCmdWithFactory(factory appleConnectFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apple",
		Short: "Configure Apple credentials for an App Store app",
	}
	cmd.AddCommand(
		newAppsAppleWorkflowCmd(true, factory),
		newAppsAppleWorkflowCmd(false, factory),
	)
	return cmd
}

func newAppsAppleWorkflowCmd(checkOnly bool, factory appleConnectFactory) *cobra.Command {
	var appleID, password, verificationCode, phoneNumber, teamID string
	var inAppKeyName, apiKeyName, vendorNumber string
	var sms, skipInAppKey, skipAPIKey, force bool
	use, short, long := "setup [app-id]", "Create Apple keys and configure an App Store app", appleSetupInstructions
	privacy := applePrivacyNotice
	if checkOnly {
		use, short, long = "check [app-id]", "Check Apple authentication and key access", appleCheckInstructions
		privacy = appleCheckPrivacyNotice
	}

	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Long:  short + ".\n\n" + long + "\n\n" + privacy,
		Args:  cobra.MaximumNArgs(1),
		Annotations: map[string]string{
			"requires_human":        "true",
			"requires_human_reason": "Signs in to Apple with an Apple ID, password, and two-factor code; the user must run it in a local interactive terminal. Never collect Apple credentials in chat.",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			appleID = valueOrEnv(appleID, "RC_APPLE_ID")
			password = valueOrEnv(password, "RC_APPLE_PASSWORD")
			verificationCode = valueOrEnv(verificationCode, "RC_APPLE_2FA_CODE")
			phoneNumber = valueOrEnv(phoneNumber, "RC_APPLE_PHONE_NUMBER")
			teamID = valueOrEnv(teamID, "RC_APPLE_TEAM_ID")
			if !checkOnly {
				vendorNumber = valueOrEnv(vendorNumber, "RC_APPLE_VENDOR_NUMBER")
			}

			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			rc, err := rt.API()
			if err != nil {
				return err
			}
			appID, err := requireID(rt, argAt(args, 0), "app", func() ([]PickerItem, error) {
				page, err := rc.Apps.List(cmd.Context(), projectID)
				if err != nil {
					return nil, err
				}
				items := make([]PickerItem, 0, len(page.Items))
				for _, app := range page.Items {
					if app.Type == "app_store" {
						items = append(items, PickerItem{ID: app.ID, Label: app.Name})
					}
				}
				return items, nil
			})
			if err != nil {
				return err
			}
			app, err := rc.Apps.Get(cmd.Context(), projectID, appID)
			if err != nil {
				return err
			}
			if app.Type != "app_store" || app.AppStore == nil {
				return fmt.Errorf("app %s is not an App Store app", appID)
			}

			existingVendor := ""
			if extras, err := rc.Apps.GetExtras(cmd.Context(), projectID, appID); err == nil {
				existingVendor = extras.AppStoreVendorNumber()
			}
			if !checkOnly {
				vendorLabel := existingVendor
				if vendorLabel == "" {
					vendorLabel = "not configured"
				}
				rt.Out.Info(fmt.Sprintf("Current Apple configuration for %s (%s):", app.Name, appID))
				rt.Out.Info("  In-app purchase key:        " + appleConfiguredLabel(app.AppStore.SubscriptionKeyConfigured))
				rt.Out.Info("  App Store Connect API key:  " + appleConfiguredLabel(app.AppStore.AppStoreConnectAPIKeyConfigured))
				rt.Out.Info("  Vendor number:              " + vendorLabel)
			}
			// Interactive setups defer the per-key decisions until after Apple
			// sign-in so they can be made against live App Store Connect state
			// (app record, vendor number). Non-interactive runs decide from
			// flags now and can exit without touching Apple.
			promptDecisions := !checkOnly && !rt.Globals.NoInput && !rt.Globals.AssumeYes && tui.IsInteractive()
			createInAppKey, createAPIKey := false, false
			if !promptDecisions {
				createInAppKey, err = decideAppleKey(rt, "in-app purchase key", app.AppStore.SubscriptionKeyConfigured, skipInAppKey, force, false)
				if err != nil {
					return err
				}
				createAPIKey, err = decideAppleKey(rt, "App Store Connect API key", app.AppStore.AppStoreConnectAPIKeyConfigured, skipAPIKey, force, false)
				if err != nil {
					return err
				}
				if !checkOnly && !createInAppKey && !createAPIKey && vendorNumber == "" {
					rt.Out.Success("Nothing to do — existing configuration was kept.")
					if rt.Out.IsJSON() {
						return rt.Out.Render(appleConfigurationResult{
							AppID:             appID,
							Mode:              "noop",
							AlreadyConfigured: true,
						})
					}
					return nil
				}
			}
			needsApple := checkOnly || promptDecisions || createInAppKey || createAPIKey

			if needsApple {
				if !rt.Globals.NoInput && tui.IsInteractive() {
					if checkOnly {
						rt.Out.Info(appleCheckInstructions)
						rt.Out.Info(privacy)
					} else {
						rt.Out.Info("Credentials go directly to Apple — never to RevenueCat. New private keys are uploaded to RevenueCat and never stored locally or shown.")
					}
				}
				if !checkOnly && !rt.Out.IsJSON() {
					rt.Out.Info("Plan:")
					step := 1
					planStep := func(text string) {
						rt.Out.Info(fmt.Sprintf("  %d. %s", step, text))
						step++
					}
					planStep("Sign in to Apple (trusted-device or SMS verification)")
					if app.AppStore.BundleID != "" {
						planStep("Verify the App Store Connect app record for " + app.AppStore.BundleID + " (offering to create it if missing)")
					}
					switch {
					case promptDecisions:
						planStep("Ask which Apple keys to create or replace (nothing changes without consent)")
					case createInAppKey && createAPIKey:
						planStep(fmt.Sprintf("Create in-app purchase key %q and App Store Connect API key %q", inAppKeyName, apiKeyName))
					case createInAppKey:
						planStep(fmt.Sprintf("Create in-app purchase key %q in App Store Connect", inAppKeyName))
					case createAPIKey:
						planStep(fmt.Sprintf("Create App Store Connect API key %q", apiKeyName))
					}
					if vendorNumber != "" {
						planStep("Set vendor number " + vendorNumber)
					} else {
						planStep("Look up your vendor number in App Store Connect and confirm before setting it")
					}
					planStep("Upload the results to RevenueCat app " + appID + " (keys are never stored locally)")
				}
				if !checkOnly && !rt.Globals.AssumeYes {
					confirmed, err := tui.Confirm(rt.Globals.NoInput, "Continue and sign in to Apple?")
					if err != nil {
						return err
					}
					if !confirmed {
						return errors.New("cancelled")
					}
				}
				if err := tui.Form(rt.Globals.NoInput).
					Field(huh.NewInput().Title("Apple Account email").Value(&appleID).Validate(tui.Required("Apple Account email"))).
					Field(huh.NewInput().Title("Apple Account password").EchoMode(huh.EchoModePassword).Value(&password).Validate(tui.Required("Apple Account password"))).
					Run(); err != nil {
					return err
				}
				if appleID == "" || password == "" {
					return errors.New("Apple sign-in needs a human at an interactive terminal (Apple ID, password, and 2FA). " +
						"Ask the user to run `rc apps apple " + map[bool]string{true: "check", false: "setup"}[checkOnly] + " " + appID + "` locally — " +
						"never collect Apple credentials in chat or pass them via flags")
				}
			}

			update := api.AppUpdate{AppStore: &api.AppStoreAppConfig{}}
			result := appleConfigurationResult{
				AppID: appID,
				Mode:  "setup",
			}
			if checkOnly {
				result.Mode = "check"
			}
			createdIDs := make([]string, 0, 2)
			if needsApple {
				apple, err := factory()
				if err != nil {
					return err
				}
				rt.Out.Info("Signing in to Apple as " + appleID + "…")
				session, err := apple.Login(cmd.Context(), appleID, password)
				if err != nil {
					var twoFactor *appleconnect.TwoFactorRequiredError
					if !errors.As(err, &twoFactor) {
						return err
					}
					challenge, err := apple.PrepareTwoFactor(cmd.Context(), session, sms, phoneNumber)
					if err != nil {
						return err
					}
					if verificationCode == "" && !rt.Globals.NoInput && tui.IsInteractive() {
						title := fmt.Sprintf("Apple %s verification code", strings.ReplaceAll(challenge.Method, "_", " "))
						if challenge.Destination != "" {
							title += " sent to " + challenge.Destination
						}
						if err := tui.Form(false).Field(huh.NewInput().Title(title).Value(&verificationCode).Validate(tui.Required("verification code"))).Run(); err != nil {
							return err
						}
					}
					if verificationCode == "" {
						return errors.New("--verification-code is required for Apple two-factor authentication")
					}
					if err := apple.CompleteTwoFactor(cmd.Context(), session, verificationCode); err != nil {
						return err
					}
				}
				if teamID == "" && len(session.Providers) > 1 {
					if rt.Globals.NoInput || !tui.IsInteractive() {
						available := make([]string, 0, len(session.Providers))
						for _, provider := range session.Providers {
							available = append(available, fmt.Sprintf("%d (%s)", provider.ID, provider.Name))
						}
						return fmt.Errorf("--team-id is required for an Apple Account with multiple providers; available: %s", strings.Join(available, ", "))
					}
					var selected int64
					options := make([]huh.Option[int64], 0, len(session.Providers))
					for _, provider := range session.Providers {
						options = append(options, huh.NewOption(fmt.Sprintf("%s (%d)", provider.Name, provider.ID), provider.ID))
					}
					if err := tui.Form(false).Field(huh.NewSelect[int64]().Title("App Store Connect team").Options(options...).Value(&selected)).Run(); err != nil {
						return err
					}
					teamID = strconv.FormatInt(selected, 10)
				}
				if teamID != "" {
					providerID, err := strconv.ParseInt(teamID, 10, 64)
					if err != nil {
						return fmt.Errorf("--team-id must be an integer: %w", err)
					}
					if err := apple.SelectProvider(cmd.Context(), session, providerID); err != nil {
						return err
					}
				}
				result.ProviderID = session.Provider.ID
				result.ProviderName = session.Provider.Name
				if checkOnly {
					if err := apple.CheckKeyAccess(cmd.Context(), session, appleconnect.InAppPurchaseKey); err != nil {
						return err
					}
					result.InAppPurchaseKeyAccess = true
					if err := apple.CheckKeyAccess(cmd.Context(), session, appleconnect.AppStoreConnectKey); err != nil {
						return err
					}
					result.AppStoreConnectKeyAccess = true
					if createInAppKey {
						result.WouldCreate = append(result.WouldCreate, string(appleconnect.InAppPurchaseKey))
					}
					if createAPIKey {
						result.WouldCreate = append(result.WouldCreate, string(appleconnect.AppStoreConnectKey))
					}
					rt.Out.Success("Apple check succeeded; no keys were created and RevenueCat was not changed")
					wouldCreate := "nothing — all keys configured"
					if len(result.WouldCreate) > 0 {
						wouldCreate = strings.Join(result.WouldCreate, ", ")
					}
					return rt.Out.RenderCard(output.Card{
						Title:    fmt.Sprintf("%s (%s)", app.Name, appID),
						Subtitle: fmt.Sprintf("App Store Connect team %s (%d)", result.ProviderName, result.ProviderID),
						Sections: []output.CardSection{{
							Heading: "Apple access",
							Lines: []output.CardLine{
								{Key: "In-app purchase keys", Value: "accessible"},
								{Key: "App Store Connect keys", Value: "accessible"},
								{Key: "A real run would create", Value: wouldCreate},
							},
						}},
						Raw: result,
					})
				}

				if err := ensureAppStoreAppRecord(cmd.Context(), rt, apple, session, app); err != nil {
					return err
				}

				if promptDecisions {
					createInAppKey, err = decideAppleKey(rt, "in-app purchase key", app.AppStore.SubscriptionKeyConfigured, skipInAppKey, force, true)
					if err != nil {
						return err
					}
					createAPIKey, err = decideAppleKey(rt, "App Store Connect API key", app.AppStore.AppStoreConnectAPIKeyConfigured, skipAPIKey, force, true)
					if err != nil {
						return err
					}
				}

				if vendorNumber == "" {
					rt.Out.Info("Looking up your vendor number in App Store Connect…")
					fetched, err := apple.FetchVendorNumber(cmd.Context(), session)
					switch {
					case err == nil && fetched == existingVendor:
						rt.Out.Info("Vendor number " + fetched + " already matches the RevenueCat app; nothing to change.")
					case err == nil:
						rt.Out.Success("Found vendor number " + fetched)
						use := true
						if !rt.Globals.NoInput && !rt.Globals.AssumeYes && tui.IsInteractive() {
							question := "Set vendor number " + fetched + " on the RevenueCat app?"
							if existingVendor != "" {
								question = "Replace vendor number " + existingVendor + " with " + fetched + " on the RevenueCat app?"
							}
							use, err = tui.ConfirmDefault(rt.Globals.NoInput, question, true)
							if err != nil {
								return err
							}
						}
						if use {
							vendorNumber = fetched
						} else {
							rt.Out.Info("Keeping the RevenueCat app's current vendor number setting.")
						}
					case !rt.Globals.NoInput && tui.IsInteractive():
						rt.Out.Warn("Could not fetch it automatically: " + err.Error())
						rt.Out.Info("Find it in App Store Connect → Payments and Financial Reports, next to your legal entity name.")
						if err := tui.Form(rt.Globals.NoInput).
							Field(huh.NewInput().Title("Vendor number (blank keeps the current setting)").Value(&vendorNumber)).
							Run(); err != nil {
							return err
						}
					default:
						rt.Out.Warn("Could not fetch the vendor number automatically; set it later with --vendor-number. (" + err.Error() + ")")
					}
				}

				if createInAppKey {
					rt.Out.Info("Creating in-app purchase key in App Store Connect…")
					key, err := apple.CreateInAppPurchaseKey(cmd.Context(), session, inAppKeyName)
					if err != nil {
						return appleConfigurationError(err, createdIDs)
					}
					rt.Out.Success(fmt.Sprintf("Created in-app purchase key %s (%q) in App Store Connect", key.ID, inAppKeyName))
					createdIDs = append(createdIDs, string(key.Kind)+":"+key.ID)
					result.InAppPurchaseKeyID = key.ID
					update.AppStore.SubscriptionPrivateKey = &key.PrivateKey
					update.AppStore.SubscriptionKeyID = &key.ID
					update.AppStore.SubscriptionKeyIssuer = &key.IssuerID
				}
				if createAPIKey {
					rt.Out.Info("Creating App Store Connect API key…")
					key, err := apple.CreateAppStoreConnectKey(cmd.Context(), session, apiKeyName)
					if err != nil {
						return appleConfigurationError(err, createdIDs)
					}
					rt.Out.Success(fmt.Sprintf("Created App Store Connect API key %s (%q)", key.ID, apiKeyName))
					createdIDs = append(createdIDs, string(key.Kind)+":"+key.ID)
					result.AppStoreConnectAPIKeyID = key.ID
					update.AppStore.AppStoreConnectAPIKey = &key.PrivateKey
					update.AppStore.AppStoreConnectAPIKeyID = &key.ID
					update.AppStore.AppStoreConnectAPIKeyIssuer = &key.IssuerID
				}
			}
			if vendorNumber != "" {
				update.AppStore.AppStoreConnectVendorNumber = &vendorNumber
				rt.Out.Info("Setting vendor number " + vendorNumber + "…")
			}
			if len(createdIDs) > 0 || vendorNumber != "" {
				rt.Out.Info("Uploading configuration to RevenueCat…")
				if _, err := rc.Apps.Update(cmd.Context(), projectID, appID, update); err != nil {
					return appleConfigurationError(fmt.Errorf("upload Apple configuration to RevenueCat: %w", err), createdIDs)
				}
			} else {
				rt.Out.Info("No RevenueCat changes to upload.")
			}
			result.VendorNumberConfigured = vendorNumber != ""
			rt.Out.Success("Apple credentials configured")
			subtitle := ""
			if result.ProviderName != "" {
				subtitle = fmt.Sprintf("App Store Connect team %s (%d)", result.ProviderName, result.ProviderID)
			}
			keptOrCreated := func(id string) string {
				if id == "" {
					return "kept existing"
				}
				return id + " (created)"
			}
			vendorLine := "unchanged"
			if vendorNumber != "" {
				vendorLine = vendorNumber
			}
			return rt.Out.RenderCard(output.Card{
				Title:    fmt.Sprintf("%s (%s)", app.Name, appID),
				Subtitle: subtitle,
				Sections: []output.CardSection{{
					Heading: "Configured",
					Lines: []output.CardLine{
						{Key: "In-app purchase key", Value: keptOrCreated(result.InAppPurchaseKeyID)},
						{Key: "App Store Connect API key", Value: keptOrCreated(result.AppStoreConnectAPIKeyID)},
						{Key: "Vendor number", Value: vendorLine},
					},
				}},
				Raw: result,
			})
		},
	}
	cmd.Flags().StringVar(&appleID, "apple-id", "", "Apple Account email (env: RC_APPLE_ID)")
	cmd.Flags().StringVar(&password, "apple-password", "", "Apple Account password; prefer the masked prompt or RC_APPLE_PASSWORD to avoid shell history and process-list exposure")
	cmd.Flags().StringVar(&verificationCode, "verification-code", "", "Apple verification code (env: RC_APPLE_2FA_CODE)")
	cmd.Flags().BoolVar(&sms, "sms", false, "send the verification code by SMS instead of a trusted device")
	cmd.Flags().StringVar(&phoneNumber, "phone-number", "", "trusted phone number for SMS (env: RC_APPLE_PHONE_NUMBER)")
	cmd.Flags().StringVar(&teamID, "team-id", "", "App Store Connect provider ID (env: RC_APPLE_TEAM_ID)")
	if !checkOnly {
		cmd.Flags().StringVar(&inAppKeyName, "in-app-key-name", "RevenueCat CLI", "name for the new in-app purchase key")
		cmd.Flags().StringVar(&apiKeyName, "api-key-name", "RevenueCat CLI", "name for the new App Store Connect API key")
		cmd.Flags().StringVar(&vendorNumber, "vendor-number", "", "App Store Connect vendor number (env: RC_APPLE_VENDOR_NUMBER)")
		cmd.Flags().BoolVar(&skipInAppKey, "skip-in-app-purchase-key", false, "do not create an in-app purchase key")
		cmd.Flags().BoolVar(&skipAPIKey, "skip-app-store-connect-key", false, "do not create an App Store Connect API key")
		cmd.Flags().BoolVar(&force, "force", false, "create new keys even when RevenueCat already has them configured")
	}
	return cmd
}

func valueOrEnv(value, name string) string {
	if value != "" {
		return value
	}
	return os.Getenv(name)
}

func appleConfigurationError(err error, createdIDs []string) error {
	if len(createdIDs) == 0 {
		return err
	}
	return fmt.Errorf("%w; Apple keys already created and cannot be downloaded again: %s", err, strings.Join(createdIDs, ", "))
}
