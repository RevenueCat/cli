package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/output"
	"github.com/revenuecat/cli/internal/tui"
)

// App types verified against fixtures. New types should be added to the
// select picker as the platform grows.
var appTypes = []huh.Option[string]{
	huh.NewOption("App Store", "app_store"),
	huh.NewOption("Play Store", "play_store"),
	huh.NewOption("Amazon", "amazon"),
	huh.NewOption("Mac App Store", "mac_app_store"),
	huh.NewOption("Roku", "roku"),
	huh.NewOption("Stripe", "stripe"),
	huh.NewOption("Web Billing (RC Billing)", "rc_billing"),
	huh.NewOption("Paddle", "paddle"),
}

func newAppsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "apps",
		Aliases: []string{"app"},
		Short:   "Manage apps in a project",
	}
	cmd.AddCommand(
		newAppsListCmd(),
		newAppsShowCmd(),
		newAppsCreateCmd(),
		newAppsUpdateCmd(),
		newAppsDeleteCmd(),
		newAppsAppleCmd(),
		newAppsKeysCmd(),
		newAppsStoreKitConfigCmd(),
	)
	return cmd
}

func newAppsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List apps",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			page, err := client.Apps.List(cmd.Context(), projectID)
			if err != nil {
				return err
			}

			if !rt.Globals.JSON && !rt.Globals.NoInput && tui.IsInteractive() {
				items := make([]tui.BrowserItem, len(page.Items))
				for i, a := range page.Items {
					items[i] = appToItem(projectID, a)
				}
				err := tui.RunBrowserTable("Apps", []string{"ID", "NAME", "TYPE", "CREATED", "CREDENTIALS"}, items)
				appleSetupHintForApps(rt, page.Items)
				return err
			}

			rows := make([][]string, 0, len(page.Items))
			for _, a := range page.Items {
				rows = append(rows, []string{a.ID, a.Name, string(a.Type), formatMillis(a.CreatedAt), appCredentialStatus(a)})
			}
			if err := rt.Out.RenderTable(output.Table{
				Columns: []string{"ID", "NAME", "TYPE", "CREATED", "CREDENTIALS"},
				Rows:    rows,
				Raw:     page,
			}); err != nil {
				return err
			}
			appleSetupHintForApps(rt, page.Items)
			return nil
		},
	}
}

func newAppsShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show [id]",
		Short: "Show an app",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			appID, err := requireID(rt, argAt(args, 0), "app", func() ([]PickerItem, error) {
				page, err := client.Apps.List(cmd.Context(), projectID)
				if err != nil {
					return nil, err
				}
				items := make([]PickerItem, len(page.Items))
				for i, a := range page.Items {
					items[i] = PickerItem{ID: a.ID, Label: fmt.Sprintf("%s  (%s)", a.Name, string(a.Type))}
				}
				return items, nil
			})
			if err != nil {
				return err
			}
			a, err := client.Apps.Get(cmd.Context(), projectID, appID)
			if err != nil {
				return err
			}
			if !rt.Globals.JSON && !rt.Globals.NoInput && tui.IsInteractive() {
				item := appToItem(projectID, *a)
				err := tui.RunBrowser("App", []tui.BrowserItem{item})
				appleSetupHintForApps(rt, []api.App{*a})
				return err
			}
			appleSetupHintForApps(rt, []api.App{*a})
			return rt.Out.Render(a)
		},
	}
}

func newAppsCreateCmd() *cobra.Command {
	var name, appType string
	var bundleID, packageName, sharedSecret string
	var subscriptionPrivateKey, subscriptionKeyID, subscriptionKeyIssuer string
	var appStoreConnectAPIKey, appStoreConnectAPIKeyID, appStoreConnectAPIKeyIssuer, appStoreConnectVendorNumber string
	var stripeAccountID, appName, defaultCurrency, supportEmail string
	var rokuAPIKey, rokuChannelID, rokuChannelName string
	var paddleAPIKey string
	var paddleSandbox bool
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an app",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			if err := tui.Form(rt.Globals.NoInput).
				Field(huh.NewInput().Title("App name").Value(&name).Validate(tui.Required("name"))).
				Field(huh.NewSelect[string]().Title("Type").Options(appTypes...).Value(&appType)).
				Run(); err != nil {
				return err
			}
			if !validAppType(appType) {
				return fmt.Errorf("--type is required: app_store|play_store|amazon|mac_app_store|roku|stripe|rc_billing|paddle")
			}
			if err := promptForAppPlatformFields(rt, appType, &bundleID, &packageName, &appName); err != nil {
				return err
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			body := api.AppCreate{Name: name, Type: appType}
			switch appType {
			case "amazon":
				body.Amazon = &api.AmazonAppConfig{PackageName: packageName, SharedSecret: ptrIfSet(sharedSecret)}
			case "app_store":
				body.AppStore = &api.AppStoreAppConfig{
					BundleID:                    bundleID,
					SharedSecret:                ptrIfSet(sharedSecret),
					SubscriptionPrivateKey:      ptrIfSet(subscriptionPrivateKey),
					SubscriptionKeyID:           ptrIfSet(subscriptionKeyID),
					SubscriptionKeyIssuer:       ptrIfSet(subscriptionKeyIssuer),
					AppStoreConnectAPIKey:       ptrIfSet(appStoreConnectAPIKey),
					AppStoreConnectAPIKeyID:     ptrIfSet(appStoreConnectAPIKeyID),
					AppStoreConnectAPIKeyIssuer: ptrIfSet(appStoreConnectAPIKeyIssuer),
					AppStoreConnectVendorNumber: ptrIfSet(appStoreConnectVendorNumber),
				}
			case "mac_app_store":
				body.MacAppStore = &api.MacAppStoreConfig{BundleID: bundleID, SharedSecret: ptrIfSet(sharedSecret)}
			case "paddle":
				body.Paddle = &api.PaddleAppConfig{
					PaddleAPIKey:    ptrIfSet(paddleAPIKey),
					PaddleIsSandbox: ptrBoolIfChanged(cmd, "paddle-sandbox", paddleSandbox),
				}
			case "play_store":
				body.PlayStore = &api.PlayStoreAppConfig{PackageName: packageName}
			case "rc_billing":
				body.RCBilling = &api.RCBillingConfig{
					AppName:         appName,
					DefaultCurrency: ptrIfSet(defaultCurrency),
					StripeAccountID: ptrIfSet(stripeAccountID),
					SupportEmail:    ptrIfSet(supportEmail),
				}
			case "roku":
				body.Roku = &api.RokuAppConfig{
					RokuAPIKey:      ptrIfSet(rokuAPIKey),
					RokuChannelID:   ptrIfSet(rokuChannelID),
					RokuChannelName: ptrIfSet(rokuChannelName),
				}
			case "stripe":
				body.Stripe = &api.StripeAppConfig{StripeAccountID: ptrIfSet(stripeAccountID)}
			}
			a, err := client.Apps.Create(cmd.Context(), projectID, body)
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Created app %s", a.ID))
			if appType == "app_store" && subscriptionPrivateKey == "" {
				rt.Out.Warn("Apple credentials are still required before App Store purchases can be validated.")
				rt.Out.Info(fmt.Sprintf("Run `rc apps apple setup %s` in a local terminal — it signs in to Apple with 2FA, so a human must run it (agents: hand this command to the user).", a.ID))
			}
			return rt.Out.Render(a)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "app name (required)")
	cmd.Flags().StringVar(&appType, "type", "", "app type: app_store|play_store|amazon|mac_app_store|roku|stripe|rc_billing|paddle")
	cmd.Flags().StringVar(&bundleID, "bundle-id", "", "Apple bundle identifier for app_store or mac_app_store")
	cmd.Flags().StringVar(&packageName, "package-name", "", "store package name for play_store or amazon")
	cmd.Flags().StringVar(&sharedSecret, "shared-secret", "", "Apple or Amazon shared secret")
	cmd.Flags().StringVar(&subscriptionPrivateKey, "subscription-private-key", "", "App Store subscription private key PEM")
	cmd.Flags().StringVar(&subscriptionKeyID, "subscription-key-id", "", "App Store subscription key ID")
	cmd.Flags().StringVar(&subscriptionKeyIssuer, "subscription-key-issuer", "", "App Store subscription key issuer ID")
	cmd.Flags().StringVar(&appStoreConnectAPIKey, "app-store-connect-api-key", "", "App Store Connect API key PEM")
	cmd.Flags().StringVar(&appStoreConnectAPIKeyID, "app-store-connect-api-key-id", "", "App Store Connect API key ID")
	cmd.Flags().StringVar(&appStoreConnectAPIKeyIssuer, "app-store-connect-api-key-issuer", "", "App Store Connect API key issuer ID")
	cmd.Flags().StringVar(&appStoreConnectVendorNumber, "app-store-connect-vendor-number", "", "App Store Connect vendor number")
	cmd.Flags().StringVar(&stripeAccountID, "stripe-account-id", "", "connected Stripe account ID for stripe or rc_billing")
	cmd.Flags().StringVar(&appName, "app-name", "", "checkout app name for rc_billing")
	cmd.Flags().StringVar(&defaultCurrency, "default-currency", "", "ISO 4217 currency code for rc_billing")
	cmd.Flags().StringVar(&supportEmail, "support-email", "", "support email for rc_billing")
	cmd.Flags().StringVar(&rokuAPIKey, "roku-api-key", "", "Roku Pay API key")
	cmd.Flags().StringVar(&rokuChannelID, "roku-channel-id", "", "Roku channel ID")
	cmd.Flags().StringVar(&rokuChannelName, "roku-channel-name", "", "Roku channel name")
	cmd.Flags().StringVar(&paddleAPIKey, "paddle-api-key", "", "Paddle server-side API key")
	cmd.Flags().BoolVar(&paddleSandbox, "paddle-sandbox", false, "mark Paddle app as sandbox")
	return cmd
}

func promptForAppPlatformFields(rt *Runtime, appType string, bundleID, packageName, appName *string) error {
	form := tui.Form(rt.Globals.NoInput)
	switch appType {
	case "amazon":
		form.Field(huh.NewInput().Title("Package name").Value(packageName).Validate(tui.Required("package name")))
	case "app_store", "mac_app_store":
		form.Field(huh.NewInput().Title("Bundle ID").Value(bundleID).Validate(tui.Required("bundle ID")))
	case "play_store":
		form.Field(huh.NewInput().Title("Package name").Value(packageName).Validate(tui.Required("package name")))
	case "rc_billing":
		form.Field(huh.NewInput().Title("App name").Value(appName).Validate(tui.Required("app name")))
	default:
		return nil
	}
	return form.Run()
}

func validAppType(appType string) bool {
	switch appType {
	case "amazon", "app_store", "mac_app_store", "paddle", "play_store", "rc_billing", "roku", "stripe":
		return true
	default:
		return false
	}
}

func newAppsUpdateCmd() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "update [id]",
		Short: "Update an app",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			appID, err := requireID(rt, argAt(args, 0), "app", func() ([]PickerItem, error) {
				page, err := client.Apps.List(cmd.Context(), projectID)
				if err != nil {
					return nil, err
				}
				items := make([]PickerItem, len(page.Items))
				for i, a := range page.Items {
					items[i] = PickerItem{ID: a.ID, Label: fmt.Sprintf("%s  (%s)", a.Name, string(a.Type))}
				}
				return items, nil
			})
			if err != nil {
				return err
			}
			body := api.AppUpdate{}
			if cmd.Flags().Changed("name") {
				body.Name = &name
			}
			a, err := client.Apps.Update(cmd.Context(), projectID, appID, body)
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Updated %s", a.ID))
			return rt.Out.Render(a)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "new name")
	return cmd
}

func newAppsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete [id]",
		Short: "Delete an app",
		Long: `Permanently deletes an app from the project. Disconnects RevenueCat
from the underlying store integration; existing customer data is retained
but no longer associated with this app.

Reversibility: irreversible.

Confirmation: prompts under TTY; pass --yes to skip. Required under --no-input.`,
		Example: `  rc apps delete app_old --yes`,
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			appID, err := requireID(rt, argAt(args, 0), "app", func() ([]PickerItem, error) {
				page, err := client.Apps.List(cmd.Context(), projectID)
				if err != nil {
					return nil, err
				}
				items := make([]PickerItem, len(page.Items))
				for i, a := range page.Items {
					items[i] = PickerItem{ID: a.ID, Label: fmt.Sprintf("%s  (%s)", a.Name, string(a.Type))}
				}
				return items, nil
			})
			if err != nil {
				return err
			}
			if !rt.Globals.AssumeYes {
				ok, err := tui.Confirm(rt.Globals.NoInput, fmt.Sprintf("Delete app %q?", appID))
				if err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("aborted")
				}
			}
			if err := client.Apps.Delete(cmd.Context(), projectID, appID); err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Deleted %s", appID))
			return rt.Out.Render(map[string]any{"ok": true, "id": appID})
		},
	}
}

// ── browser helpers ──────────────────────────────────────────────────────────

func appToItem(projectID string, a api.App) tui.BrowserItem {
	return tui.BrowserItem{
		ID:     a.ID,
		Label:  a.Name,
		Meta:   string(a.Type),
		Row:    []string{a.ID, a.Name, string(a.Type), formatMillis(a.CreatedAt), appCredentialStatus(a)},
		WebURL: fmt.Sprintf("https://app.revenuecat.com/projects/%s/apps/%s", dashboardProjectID(projectID), a.ID),
		Fields: []tui.BrowserField{
			{Key: "ID", Value: a.ID},
			{Key: "Name", Value: a.Name},
			{Key: "Type", Value: string(a.Type)},
			{Key: "Created", Value: formatMillis(a.CreatedAt)},
			{Key: "Credentials", Value: appCredentialStatus(a)},
		},
	}
}

// appCredentialStatus summarizes store-credential readiness for list views.
// Only App Store apps have CLI-checkable credential state today.
func appCredentialStatus(a api.App) string {
	if string(a.Type) != "app_store" || a.AppStore == nil {
		return "—"
	}
	switch {
	case a.AppStore.SubscriptionKeyConfigured && a.AppStore.AppStoreConnectAPIKeyConfigured:
		return "ready"
	case a.AppStore.SubscriptionKeyConfigured || a.AppStore.AppStoreConnectAPIKeyConfigured:
		return "partial — run: rc apps apple setup"
	default:
		return "missing — run: rc apps apple setup"
	}
}

// appleSetupHintForApps explains the CREDENTIALS column when any App Store
// app still needs Apple keys: without them RevenueCat cannot validate App
// Store purchases or manage products.
func appleSetupHintForApps(rt *Runtime, apps []api.App) {
	for _, a := range apps {
		if string(a.Type) != "app_store" || a.AppStore == nil {
			continue
		}
		if !a.AppStore.SubscriptionKeyConfigured || !a.AppStore.AppStoreConnectAPIKeyConfigured {
			rt.Out.Warn(fmt.Sprintf("%s is missing Apple credentials — App Store purchases can't be validated until they're set.", a.ID))
			rt.Out.Info(fmt.Sprintf("Run `rc apps apple setup %s` in a local terminal (interactive Apple sign-in with 2FA).", a.ID))
		}
	}
}

func newAppsKeysCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "keys [app-id]",
		Short: "List public SDK API keys for an app",
		Long: `Lists the typed public API keys used to configure RevenueCat SDKs.
These are client-side public keys, not secret RevenueCat API keys.`,
		Example: `  rc apps keys app_abc
  rc apps keys app_abc --json --no-input`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			appID, err := requireID(rt, argAt(args, 0), "app", func() ([]PickerItem, error) {
				page, err := client.Apps.List(cmd.Context(), projectID)
				if err != nil {
					return nil, err
				}
				items := make([]PickerItem, len(page.Items))
				for i, a := range page.Items {
					items[i] = PickerItem{ID: a.ID, Label: fmt.Sprintf("%s  (%s)", a.Name, string(a.Type))}
				}
				return items, nil
			})
			if err != nil {
				return err
			}
			keys, err := client.Apps.PublicAPIKeys(cmd.Context(), projectID, appID)
			if err != nil {
				return err
			}
			rows := make([][]string, len(keys.Items))
			rawItems := make([]map[string]any, len(keys.Items))
			for i, key := range keys.Items {
				keyType := publicSDKKeyType(key.Key)
				rows[i] = []string{key.ID, keyType, string(key.Environment), key.Key, formatMillis(key.CreatedAt)}
				rawItems[i] = map[string]any{
					"id": key.ID, "object": key.Object, "app_id": key.AppID, "key": key.Key,
					"key_type": keyType, "environment": key.Environment, "created_at": key.CreatedAt,
				}
			}
			// The custom URL scheme belongs with SDK integration values: apps
			// register it for paywall preview and redemption deep links.
			scheme := ""
			if extras, err := client.Apps.GetExtras(cmd.Context(), projectID, appID); err == nil {
				scheme = extras.CustomURLScheme
			}
			if err := rt.Out.RenderTable(output.Table{
				Columns: []string{"ID", "STORE / KEY TYPE", "API ENVIRONMENT", "PUBLIC SDK KEY", "CREATED"},
				Rows:    rows,
				Raw: map[string]any{
					"object": keys.Object, "items": rawItems, "next_page": keys.NextPage, "url": keys.URL,
					"custom_url_scheme": scheme,
				},
			}); err != nil {
				return err
			}
			if scheme != "" {
				rt.Out.Info("Custom URL scheme (register for paywall preview / redemption links): " + scheme)
			}
			return nil
		},
	}
}

func publicSDKKeyType(key string) string {
	switch {
	case strings.HasPrefix(key, "test_"):
		return "Test Store"
	case strings.HasPrefix(key, "appl_"):
		return "App Store"
	case strings.HasPrefix(key, "goog_"):
		return "Google Play"
	case strings.HasPrefix(key, "amaz_"):
		return "Amazon Appstore"
	case strings.HasPrefix(key, "strp_"):
		return "Stripe"
	default:
		return "Public SDK"
	}
}

func newAppsStoreKitConfigCmd() *cobra.Command {
	var outputPath string
	cmd := &cobra.Command{
		Use:     "storekit-config [app-id]",
		Aliases: []string{"storekit"},
		Short:   "Export the StoreKit configuration for an app",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			appID, err := requireID(rt, argAt(args, 0), "app", func() ([]PickerItem, error) {
				page, err := client.Apps.List(cmd.Context(), projectID)
				if err != nil {
					return nil, err
				}
				items := make([]PickerItem, len(page.Items))
				for i, a := range page.Items {
					items[i] = PickerItem{ID: a.ID, Label: fmt.Sprintf("%s  (%s)", a.Name, string(a.Type))}
				}
				return items, nil
			})
			if err != nil {
				return err
			}
			cfg, err := client.Apps.StoreKitConfig(cmd.Context(), projectID, appID)
			if err != nil {
				return err
			}
			if outputPath != "" {
				b, err := json.MarshalIndent(cfg.Contents, "", "  ")
				if err != nil {
					return err
				}
				b = append(b, '\n')
				if err := os.WriteFile(outputPath, b, 0o600); err != nil {
					return err
				}
				rt.Out.Success(fmt.Sprintf("Wrote %s", outputPath))
				return rt.Out.Render(map[string]any{"ok": true, "app_id": appID, "path": outputPath})
			}
			return rt.Out.Render(cfg)
		},
	}
	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "write StoreKit JSON contents to a file")
	return cmd
}

func ptrIfSet(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func ptrBoolIfChanged(cmd *cobra.Command, name string, v bool) *bool {
	if !cmd.Flags().Changed(name) {
		return nil
	}
	return &v
}
