package api

// These types are hand-written. The v2 spec now models App as a polymorphic
// oneOf union, which oapi-codegen renders as an opaque json.RawMessage wrapper —
// but the wire format is still a flat object, so we keep the previous flat shape
// here and exclude App / AppType / AppObject from codegen (oapi-codegen.yaml).

type AppType string

type AppObject string

type App struct {
	// Amazon Amazon type details
	Amazon *struct {
		// PackageName The package name of the app
		PackageName string `json:"package_name"`
	} `json:"amazon,omitempty"`

	// AppStore App Store type details
	AppStore *struct {
		// AppStoreConnectAPIKeyConfigured Whether App Store Connect API key credentials are configured.
		//
		// Example: true
		AppStoreConnectAPIKeyConfigured bool `json:"app_store_connect_api_key_configured"`

		// BundleID The bundle ID of the app
		BundleID string `json:"bundle_id"`

		// SubscriptionKeyConfigured Whether In-App Purchase subscription key credentials are configured.
		//
		// Example: true
		SubscriptionKeyConfigured bool `json:"subscription_key_configured"`
	} `json:"app_store,omitempty"`

	// CreatedAt The date when the app was created in ms since epoch
	//
	// Example: 1658399423658
	CreatedAt int64 `json:"created_at"`

	// ID The id of the app
	//
	// Example: app1a2b3c4
	ID string `json:"id"`

	// MacAppStore Legacy Mac App Store type details
	MacAppStore *struct {
		// BundleID The bundle ID of the app
		BundleID string `json:"bundle_id"`
	} `json:"mac_app_store,omitempty"`

	// Name The name of the app
	Name string `json:"name"`

	// Object String representing the object's type. Objects of the same type share the same value.
	Object AppObject `json:"object"`

	// Paddle Paddle Billing type details
	Paddle *struct {
		// PaddleAPIKey Paddle Server-side API key provided on the Paddle dashboard.
		PaddleAPIKey *string `json:"paddle_api_key,omitempty"`

		// PaddleIsSandbox Whether the app is tied to the sandbox environment.
		//
		// Example: true
		PaddleIsSandbox *bool `json:"paddle_is_sandbox,omitempty"`
	} `json:"paddle,omitempty"`

	// PlayStore Play Store type details
	PlayStore *struct {
		// PackageName The package name of the app
		PackageName string `json:"package_name"`
	} `json:"play_store,omitempty"`

	// ProjectID The id of the project
	//
	// Example: proj1a2b3c4
	ProjectID string `json:"project_id"`

	// RcBilling Revenue Cat Billing Store type details
	RcBilling *struct {
		// AppName Shown in checkout, emails, and receipts sent to customers.
		AppName *string `json:"app_name,omitempty"`

		// DefaultCurrency ISO 4217 currency code
		//
		// Example: USD
		DefaultCurrency RCBillingCurrency `json:"default_currency"`

		// SellerCompanyName The company name.  This field is deprecated. Please, use `app_name` instead.
		// Deprecated: this property has been marked as deprecated upstream, but no `x-deprecated-reason` was set
		SellerCompanyName string `json:"seller_company_name"`

		// SellerCompanySupportEmail The company support email. This field is deprecated. Please, use `support_email` instead.
		// Deprecated: this property has been marked as deprecated upstream, but no `x-deprecated-reason` was set
		SellerCompanySupportEmail *string `json:"seller_company_support_email,omitempty"`

		// StripeAccountID Stripe account connected to your RevenueCat account.
		StripeAccountID *string `json:"stripe_account_id,omitempty"`

		// SupportEmail Used as the `reply to` address in all emails sent to customers, to allow them to receive support.
		SupportEmail *string `json:"support_email,omitempty"`
	} `json:"rc_billing,omitempty"`

	// Roku Roku Channel Store type details
	Roku *struct {
		// RokuChannelID Channel ID provided on the Roku Channel page.
		RokuChannelID *string `json:"roku_channel_id,omitempty"`

		// RokuChannelName Channel name that is displayed on the Roku Channel page.
		RokuChannelName *string `json:"roku_channel_name,omitempty"`
	} `json:"roku,omitempty"`

	// Stripe Stripe type details
	Stripe *struct {
		// StripeAccountID Stripe account connected to your RevenueCat account.
		StripeAccountID *string `json:"stripe_account_id,omitempty"`
	} `json:"stripe,omitempty"`

	// Type The platform of the app
	//
	// Example: app_store
	Type                 AppType                `json:"type"`
	AdditionalProperties map[string]interface{} `json:"-"`
}
