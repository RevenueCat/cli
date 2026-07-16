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
}

type appleConnectFactory func() (appleConnectClient, error)

func newAppleConnectClient() (appleConnectClient, error) {
	return appleconnect.New(appleconnect.Options{})
}

const appleSetupInstructions = `This setup will:
  1. Sign in directly to Apple with your Apple Account.
  2. Ask for trusted-device or SMS verification when Apple requires it.
  3. Create only the Apple keys missing from this RevenueCat app.
  4. Download each private key once and upload it directly to RevenueCat.

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

			createInAppKey := !skipInAppKey && (force || !app.AppStore.SubscriptionKeyConfigured)
			createAPIKey := !skipAPIKey && (force || !app.AppStore.AppStoreConnectAPIKeyConfigured)
			needsApple := checkOnly || createInAppKey || createAPIKey
			if !needsApple && vendorNumber == "" {
				rt.Out.Success(fmt.Sprintf("App %s already has all Apple keys configured — nothing to create.", appID))
				rt.Out.Info("Pass --force to create fresh keys anyway (e.g. after revoking the old ones in App Store Connect).")
				return rt.Out.Render(appleConfigurationResult{
					AppID:             appID,
					Mode:              "noop",
					AlreadyConfigured: true,
				})
			}

			if needsApple {
				if !rt.Globals.NoInput && tui.IsInteractive() {
					if checkOnly {
						rt.Out.Info(appleCheckInstructions)
					} else {
						rt.Out.Info(appleSetupInstructions)
					}
					rt.Out.Info(privacy)
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
					return errors.New("--apple-id and --apple-password are required for Apple authentication")
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
					return rt.Out.Render(result)
				}

				if createInAppKey {
					key, err := apple.CreateInAppPurchaseKey(cmd.Context(), session, inAppKeyName)
					if err != nil {
						return appleConfigurationError(err, createdIDs)
					}
					createdIDs = append(createdIDs, string(key.Kind)+":"+key.ID)
					result.InAppPurchaseKeyID = key.ID
					update.AppStore.SubscriptionPrivateKey = &key.PrivateKey
					update.AppStore.SubscriptionKeyID = &key.ID
					update.AppStore.SubscriptionKeyIssuer = &key.IssuerID
				}
				if createAPIKey {
					key, err := apple.CreateAppStoreConnectKey(cmd.Context(), session, apiKeyName)
					if err != nil {
						return appleConfigurationError(err, createdIDs)
					}
					createdIDs = append(createdIDs, string(key.Kind)+":"+key.ID)
					result.AppStoreConnectAPIKeyID = key.ID
					update.AppStore.AppStoreConnectAPIKey = &key.PrivateKey
					update.AppStore.AppStoreConnectAPIKeyID = &key.ID
					update.AppStore.AppStoreConnectAPIKeyIssuer = &key.IssuerID
				}
			}
			if vendorNumber != "" {
				update.AppStore.AppStoreConnectVendorNumber = &vendorNumber
			}
			if _, err := rc.Apps.Update(cmd.Context(), projectID, appID, update); err != nil {
				return appleConfigurationError(fmt.Errorf("upload Apple configuration to RevenueCat: %w", err), createdIDs)
			}
			result.VendorNumberConfigured = vendorNumber != ""
			rt.Out.Success(fmt.Sprintf("Configured Apple credentials for app %s", appID))
			return rt.Out.Render(result)
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
