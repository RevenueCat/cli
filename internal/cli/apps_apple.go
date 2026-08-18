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
	Failed                   []string `json:"failed_keys,omitempty"`
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
	if !rt.CanPrompt() {
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
	return createAppStoreAppRecord(ctx, rt, apple, session, bundleID, name, sku)
}

// createAppStoreAppRecord registers the bundle ID and creates the ASC app.
// Failures here never abort: keys are account-level, so key setup still
// proceeds — the caller only warns.
func createAppStoreAppRecord(ctx context.Context, rt *Runtime, apple appleConnectClient, session *appleconnect.Session, bundleID, name, sku string) error {
	rt.Out.Info("Registering bundle ID " + bundleID + " in the Developer Portal…")
	if err := apple.RegisterBundleID(ctx, session, bundleID, name); err != nil {
		warnAppRecordFailed(rt, bundleID, err)
		return nil
	}
	rt.Out.Info("Creating the App Store Connect app…")
	if err := apple.CreateApp(ctx, session, name, bundleID, sku); err != nil {
		warnAppRecordFailed(rt, bundleID, err)
		return nil
	}
	rt.Out.Success("Created App Store Connect app " + name + " (" + bundleID + ")")
	return nil
}

// A nil key field means it was not requested or Apple refused it; Failed lists the refusals.
type createdAppleKeys struct {
	InAppPurchaseKey   *appleconnect.Key
	AppStoreConnectKey *appleconnect.Key
	Failed             []string
}

// One source for both labels so the card and the Failed list can't desync.
const (
	inAppKeyLabel = "in-app purchase key"
	ascKeyLabel   = "App Store Connect API key"
)

func createAppleKeys(ctx context.Context, rt *Runtime, apple appleConnectClient, session *appleconnect.Session, createInApp, createAPI bool, inAppKeyName, apiKeyName string) createdAppleKeys {
	var keys createdAppleKeys
	if createInApp {
		rt.Out.Info("Creating in-app purchase key in App Store Connect…")
		key, err := apple.CreateInAppPurchaseKey(ctx, session, inAppKeyName)
		switch {
		case err != nil:
			rt.Out.Warn("Could not create the " + inAppKeyLabel + ": " + err.Error())
			appleKeyHint(rt, err, appleconnect.InAppPurchaseKey)
			keys.Failed = append(keys.Failed, inAppKeyLabel)
		default:
			rt.Out.Success(fmt.Sprintf("Created in-app purchase key %s (%q) in App Store Connect", key.ID, inAppKeyName))
			keys.InAppPurchaseKey = key
		}
	}
	if createAPI {
		rt.Out.Info("Creating App Store Connect API key…")
		key, err := apple.CreateAppStoreConnectKey(ctx, session, apiKeyName)
		switch {
		case err != nil:
			rt.Out.Warn("Could not create the " + ascKeyLabel + ": " + err.Error())
			appleKeyHint(rt, err, appleconnect.AppStoreConnectKey)
			keys.Failed = append(keys.Failed, ascKeyLabel)
		default:
			rt.Out.Success(fmt.Sprintf("Created App Store Connect API key %s (%q)", key.ID, apiKeyName))
			keys.AppStoreConnectKey = key
		}
	}
	return keys
}

func appleKeyHint(rt *Runtime, err error, kind appleconnect.KeyKind) {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "maximum number of active keys") || strings.Contains(msg, "revoke"):
		rt.Out.Hint("You're at Apple's limit of active keys. Revoke an unused one in App Store Connect (Users and Access → Integrations) and re-run — or reuse a key you already set up for another app (Apple keys are account-level).")
	case kind == appleconnect.InAppPurchaseKey && (strings.Contains(msg, "does not allow") || strings.Contains(msg, "forbidden")):
		rt.Out.Hint("Apple wouldn't create the In-App Purchase key. It usually already exists (Apple allows one per account) or your Apple Account isn't the Account Holder — reuse the existing key or sign in as the Account Holder.")
	}
}

func warnAppRecordFailed(rt *Runtime, bundleID string, err error) {
	if strings.Contains(strings.ToLower(err.Error()), "not available") {
		rt.Out.Warn("Bundle ID " + bundleID + " is not available — it is likely already registered under another Apple team or reserved, so the app record could not be created here.")
	} else {
		rt.Out.Warn("Could not create the App Store Connect app record: " + err.Error())
	}
	rt.Out.Warn("Continuing with key setup. The earlier check looks for the App Store Connect app record, not Developer Portal bundle registration — create the app in App Store Connect before configuring products.")
}

// keyDecisionLabel names the user's choice for a key so the transcript
// records it after the prompt disappears.
func keyDecisionLabel(create, configured bool) string {
	switch {
	case create && configured:
		return "replace"
	case create:
		return "create"
	case configured:
		return "keep existing"
	default:
		return "skip"
	}
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
	case !mayPrompt || rt.Globals.AssumeYes || !rt.CanPrompt():
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
		Short: "Set up Apple credentials for an App Store app",
		Long: `Signs in to App Store Connect and configures the Apple credentials RevenueCat
needs for an App Store app — the in-app purchase key (validates purchases), the
App Store Connect API key (manages Products and prices), and your vendor number.

Requires an interactive terminal and Apple 2FA: a human must run it. Apple
credentials go straight to Apple and are never saved or sent to RevenueCat. Use
check for a read-only dry run before setup.`,
		Example: `  rc apps apple check app_x     # read-only: verify sign-in and key access
  rc apps apple setup app_x     # create keys and configure the app`,
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
	example := "  rc apps apple setup app_x\n  rc apps apple setup app_x --sms --phone-number +15551234567"
	if checkOnly {
		use, short, long = "check [app-id]", "Check Apple authentication and key access", appleCheckInstructions
		privacy = appleCheckPrivacyNotice
		example = "  rc apps apple check app_x"
	}

	cmd := &cobra.Command{
		Use:     use,
		Short:   short,
		Long:    short + ".\n\n" + long + "\n\n" + privacy,
		Example: example,
		Args:    cobra.MaximumNArgs(1),
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
				rt.Out.Title("Apple configuration — " + app.Name)
				rt.Out.Lead("Signs in to App Store Connect and sets up the keys RevenueCat needs — nothing changes without your OK.")
				rt.Out.Field("App", appID)
				rt.Out.Field("In-app purchase key", appleConfiguredLabel(app.AppStore.SubscriptionKeyConfigured), "validates App Store purchases")
				rt.Out.Field("App Store Connect API key", appleConfiguredLabel(app.AppStore.AppStoreConnectAPIKeyConfigured), "manages products and prices")
				rt.Out.Field("Vendor number", vendorLabel, "links sales reports to revenue charts")
			}
			// Interactive setups defer the per-key decisions until after Apple
			// sign-in so they can be made against live App Store Connect state
			// (app record, vendor number). Non-interactive runs decide from
			// flags now and can exit without touching Apple.
			promptDecisions := !checkOnly && !rt.Globals.AssumeYes && rt.CanPrompt()
			// A fully configured app gets an exit ramp before any Apple
			// sign-in: replacing working keys is the exception, not the
			// default. Explicit intent (--force, --vendor-number) skips it.
			allConfigured := app.AppStore.SubscriptionKeyConfigured &&
				app.AppStore.AppStoreConnectAPIKeyConfigured && existingVendor != ""
			if promptDecisions && allConfigured && !force && vendorNumber == "" {
				cont, err := tui.ConfirmDefault(rt.Globals.NoInput, "Everything is already configured. Continue and replace parts of it?", false)
				if err != nil {
					return err
				}
				if !cont {
					rt.Out.Success("Kept the existing configuration.")
					return nil
				}
			}
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
					}
				}
				if !checkOnly && !rt.Out.IsJSON() {
					steps := []string{"Sign in to App Store Connect with your Apple Account"}
					if app.AppStore.BundleID != "" {
						steps = append(steps, "Verify the App Store Connect app for "+app.AppStore.BundleID)
					}
					switch {
					case promptDecisions:
						steps = append(steps, "Choose which keys to create or replace")
					case createInAppKey && createAPIKey:
						steps = append(steps, "Create the in-app purchase and App Store Connect API keys")
					case createInAppKey:
						steps = append(steps, "Create the in-app purchase key")
					case createAPIKey:
						steps = append(steps, "Create the App Store Connect API key")
					}
					switch {
					case vendorNumber != "":
						steps = append(steps, "Set vendor number "+vendorNumber)
					case existingVendor != "":
						steps = append(steps, "Check the vendor number against Apple")
					default:
						steps = append(steps, "Look up and confirm the vendor number")
					}
					steps = append(steps, "Save to RevenueCat")
					rt.Out.Plan(steps)
				}
				if rt.CanPrompt() {
					if !checkOnly && !rt.Globals.AssumeYes {
						confirmed, err := tui.Confirm(rt.Globals.NoInput, "Sign in to App Store Connect now?")
						if err != nil {
							return err
						}
						if !confirmed {
							return errors.New("cancelled")
						}
					}
					if !rt.Globals.NoInput && tui.IsInteractive() {
						rt.Out.Notice(
							"Your Apple email and password go only to Apple and are never saved —",
							"RevenueCat never sees them.",
							"Apple keys created here upload straight to your RevenueCat project — they are",
							"never saved on this computer or displayed.",
						)
					}
					if err := tui.Form(rt.Globals.NoInput).
						Field(huh.NewInput().Title("Apple Account email").Value(&appleID).Validate(tui.Required("Apple Account email"))).
						Field(huh.NewInput().Title("Apple Account password").EchoMode(huh.EchoModePassword).Value(&password).Validate(tui.Required("Apple Account password"))).
						Run(); err != nil {
						return err
					}
				}
				if appleID == "" || password == "" {
					return fmt.Errorf("can't prompt for Apple sign-in here (--json or --no-input): supply the Apple Account email and password with --apple-id/--apple-password (or RC_APPLE_ID/RC_APPLE_PASSWORD) and the 2FA code with --verification-code (or RC_APPLE_2FA_CODE), or run `rc apps apple %s %s` in an interactive terminal",
						map[bool]string{true: "check", false: "setup"}[checkOnly], appID)
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
			var failedKeys []string
			if needsApple {
				apple, err := factory()
				if err != nil {
					return err
				}
				rt.Out.Blank()
				rt.Out.Info("Signing in to App Store Connect as " + appleID + "…")
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
					if verificationCode == "" && rt.CanPrompt() {
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
					if !rt.CanPrompt() {
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
				if session.Provider.Name != "" {
					rt.Out.Answer("Team", fmt.Sprintf("%s (%d)", session.Provider.Name, session.Provider.ID))
				}
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
					rt.Out.Answer("In-app purchase key", keyDecisionLabel(createInAppKey, app.AppStore.SubscriptionKeyConfigured))
					createAPIKey, err = decideAppleKey(rt, "App Store Connect API key", app.AppStore.AppStoreConnectAPIKeyConfigured, skipAPIKey, force, true)
					if err != nil {
						return err
					}
					rt.Out.Answer("App Store Connect API key", keyDecisionLabel(createAPIKey, app.AppStore.AppStoreConnectAPIKeyConfigured))
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
						if !rt.Globals.AssumeYes && rt.CanPrompt() {
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
							rt.Out.Answer("Vendor number", fetched)
						} else {
							rt.Out.Answer("Vendor number", "unchanged")
						}
					case rt.CanPrompt():
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

				keys := createAppleKeys(cmd.Context(), rt, apple, session, createInAppKey, createAPIKey, inAppKeyName, apiKeyName)
				failedKeys = keys.Failed
				if key := keys.InAppPurchaseKey; key != nil {
					createdIDs = append(createdIDs, string(key.Kind)+":"+key.ID)
					result.InAppPurchaseKeyID = key.ID
					update.AppStore.SubscriptionPrivateKey = &key.PrivateKey
					update.AppStore.SubscriptionKeyID = &key.ID
					update.AppStore.SubscriptionKeyIssuer = &key.IssuerID
				}
				if key := keys.AppStoreConnectKey; key != nil {
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
				rt.Out.Info("Saving to RevenueCat…")
				if _, err := rc.Apps.Update(cmd.Context(), projectID, appID, update); err != nil {
					return appleConfigurationError(fmt.Errorf("upload Apple configuration to RevenueCat: %w", err), createdIDs)
				}
			} else {
				rt.Out.Info("No RevenueCat changes to upload.")
			}
			result.VendorNumberConfigured = vendorNumber != ""
			result.Failed = failedKeys
			if len(failedKeys) > 0 {
				rt.Out.Warn("Partly done: couldn't create the " + strings.Join(failedKeys, " and the ") + ". What succeeded was saved to RevenueCat. A failed attempt can still leave a key in App Store Connect that counts toward Apple's limit — check Users and Access → Integrations and reuse or revoke it before re-running.")
			} else {
				rt.Out.Success("Apple credentials configured")
			}
			subtitle := ""
			if result.ProviderName != "" {
				subtitle = fmt.Sprintf("App Store Connect team %s (%d)", result.ProviderName, result.ProviderID)
			}
			keptOrCreated := func(id, label string, wasConfigured bool) string {
				for _, f := range failedKeys {
					if f == label {
						// A failed create leaves RevenueCat untouched, so a key that
						// was already configured is still in place, not gone.
						if wasConfigured {
							return "kept existing (replace failed — see note above)"
						}
						return "not created — see note above"
					}
				}
				if id == "" {
					return "kept existing"
				}
				return id + " (created)"
			}
			inAppWasConfigured := app.AppStore != nil && app.AppStore.SubscriptionKeyConfigured
			ascWasConfigured := app.AppStore != nil && app.AppStore.AppStoreConnectAPIKeyConfigured
			vendorLine := "unchanged"
			if vendorNumber != "" {
				vendorLine = vendorNumber
			}
			if err := rt.Out.RenderCard(output.Card{
				Title:    fmt.Sprintf("%s (%s)", app.Name, appID),
				Subtitle: subtitle,
				Sections: []output.CardSection{{
					Heading: "Configured",
					Lines: []output.CardLine{
						{Key: "In-app purchase key", Value: keptOrCreated(result.InAppPurchaseKeyID, inAppKeyLabel, inAppWasConfigured)},
						{Key: "App Store Connect API key", Value: keptOrCreated(result.AppStoreConnectAPIKeyID, ascKeyLabel, ascWasConfigured)},
						{Key: "Vendor number", Value: vendorLine},
					},
				}},
				Raw: result,
			}); err != nil {
				return err
			}
			// Partial progress is saved, but a refused key still means the run
			// didn't do what was asked — exit non-zero so scripts don't see success.
			if len(failedKeys) > 0 {
				return &SilentExitError{Code: 1}
			}
			return nil
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
