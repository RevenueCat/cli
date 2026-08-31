package api

import (
	"context"
	"net/http"
	"net/url"
)

type AppsService struct{ c *Client }

// AppCreate and AppUpdate are request bodies used by Create/Update methods.
// The App, AppType etc. response types are generated in types_gen.go.

type AppCreate struct {
	Name        string              `json:"name"`
	Type        string              `json:"type"`
	Amazon      *AmazonAppConfig    `json:"amazon,omitempty"`
	AppStore    *AppStoreAppConfig  `json:"app_store,omitempty"`
	MacAppStore *MacAppStoreConfig  `json:"mac_app_store,omitempty"`
	Paddle      *PaddleAppConfig    `json:"paddle,omitempty"`
	PlayStore   *PlayStoreAppConfig `json:"play_store,omitempty"`
	RCBilling   *RCBillingConfig    `json:"rc_billing,omitempty"`
	Roku        *RokuAppConfig      `json:"roku,omitempty"`
	Stripe      *StripeAppConfig    `json:"stripe,omitempty"`
}

type AppUpdate struct {
	Name      *string             `json:"name,omitempty"`
	AppStore  *AppStoreAppConfig  `json:"app_store,omitempty"`
	PlayStore *PlayStoreAppConfig `json:"play_store,omitempty"`
}

// AppExtras carries App read-model fields added after the generated types
// were last regenerated: the custom URL scheme used for paywall preview /
// redemption deep links, and the App Store vendor number.
// Both are empty when unset or when the server predates the fields.
type AppExtras struct {
	CustomURLScheme string `json:"custom_url_scheme,omitempty"`
	AppStore        *struct {
		VendorNumber string `json:"app_store_connect_vendor_number,omitempty"`
	} `json:"app_store,omitempty"`
}

func (e *AppExtras) AppStoreVendorNumber() string {
	if e == nil || e.AppStore == nil {
		return ""
	}
	return e.AppStore.VendorNumber
}

// GetExtras fetches the newer read-model fields for an app.
func (s *AppsService) GetExtras(ctx context.Context, projectID, id string) (*AppExtras, error) {
	var out AppExtras
	err := s.c.do(ctx, http.MethodGet, pathApp(projectID, id), nil, &out)
	return &out, err
}

type AmazonAppConfig struct {
	PackageName  string  `json:"package_name"`
	SharedSecret *string `json:"shared_secret,omitempty"`
}

type AppStoreAppConfig struct {
	BundleID                    string  `json:"bundle_id,omitempty"`
	SharedSecret                *string `json:"shared_secret,omitempty"`
	SubscriptionPrivateKey      *string `json:"subscription_private_key,omitempty"`
	SubscriptionKeyID           *string `json:"subscription_key_id,omitempty"`
	SubscriptionKeyIssuer       *string `json:"subscription_key_issuer,omitempty"`
	AppStoreConnectAPIKey       *string `json:"app_store_connect_api_key,omitempty"`
	AppStoreConnectAPIKeyID     *string `json:"app_store_connect_api_key_id,omitempty"`
	AppStoreConnectAPIKeyIssuer *string `json:"app_store_connect_api_key_issuer,omitempty"`
	AppStoreConnectVendorNumber *string `json:"app_store_connect_vendor_number,omitempty"`
}

type MacAppStoreConfig struct {
	BundleID     string  `json:"bundle_id"`
	SharedSecret *string `json:"shared_secret,omitempty"`
}

type PaddleAppConfig struct {
	PaddleAPIKey    *string `json:"paddle_api_key,omitempty"`
	PaddleIsSandbox *bool   `json:"paddle_is_sandbox,omitempty"`
}

type PlayStoreAppConfig struct {
	PackageName string `json:"package_name,omitempty"`
	// PlayServiceAccountCredentialsJSON is the Google service-account key JSON,
	// write-only. Set on an app update to upload the Play credential. The read
	// model never returns it — see PlayServiceAccountCredentialsConfigured.
	PlayServiceAccountCredentialsJSON *string `json:"play_service_account_credentials_json,omitempty"`
}

type RCBillingConfig struct {
	AppName         string  `json:"app_name"`
	DefaultCurrency *string `json:"default_currency,omitempty"`
	StripeAccountID *string `json:"stripe_account_id,omitempty"`
	SupportEmail    *string `json:"support_email,omitempty"`
}

type RokuAppConfig struct {
	RokuAPIKey      *string `json:"roku_api_key,omitempty"`
	RokuChannelID   *string `json:"roku_channel_id,omitempty"`
	RokuChannelName *string `json:"roku_channel_name,omitempty"`
}

type StripeAppConfig struct {
	StripeAccountID *string `json:"stripe_account_id,omitempty"`
}

// List follows next_page until the project's apps are drained, like
// Projects.List — callers (pickers, org-wide identifier lookups) treat the
// result as the complete set, so a single-page fetch would silently truncate.
// Object/URL come from the first page; NextPage is always empty on return.
func (s *AppsService) List(ctx context.Context, projectID string) (*Page[App], error) {
	var out Page[App]
	path := pathApps(projectID)
	for {
		var page Page[App]
		if err := s.c.do(ctx, http.MethodGet, path, nil, &page); err != nil {
			return nil, err
		}
		if out.Object == "" {
			out.Object, out.URL = page.Object, page.URL
		}
		out.Items = append(out.Items, page.Items...)
		cursor := page.NextCursor()
		if cursor == "" {
			break
		}
		path = pathApps(projectID) + "?starting_after=" + url.QueryEscape(cursor)
	}
	return &out, nil
}

func (s *AppsService) Get(ctx context.Context, projectID, id string) (*App, error) {
	var out App
	err := s.c.do(ctx, http.MethodGet, pathApp(projectID, id), nil, &out)
	return &out, err
}

func (s *AppsService) Create(ctx context.Context, projectID string, body AppCreate) (*App, error) {
	var out App
	err := s.c.do(ctx, http.MethodPost, pathApps(projectID), body, &out)
	return &out, err
}

func (s *AppsService) Update(ctx context.Context, projectID, id string, body AppUpdate) (*App, error) {
	var out App
	err := s.c.do(ctx, http.MethodPost, pathApp(projectID, id), body, &out)
	return &out, err
}

func (s *AppsService) Delete(ctx context.Context, projectID, id string) error {
	return s.c.do(ctx, http.MethodDelete, pathApp(projectID, id), nil, nil)
}

func (s *AppsService) PublicAPIKeys(ctx context.Context, projectID, appID string) (*ListPublicAPIKeys, error) {
	var out ListPublicAPIKeys
	err := s.c.do(ctx, http.MethodGet, pathAppPublicAPIKeys(projectID, appID), nil, &out)
	return &out, err
}

// GET /projects/{project_id}/apps/{app_id}/store_kit_config
func (s *AppsService) StoreKitConfig(ctx context.Context, projectID, appID string) (*StoreKitConfigFile, error) {
	var out StoreKitConfigFile
	err := s.c.do(ctx, http.MethodGet, pathAppStoreKitConfig(projectID, appID), nil, &out)
	return &out, err
}
