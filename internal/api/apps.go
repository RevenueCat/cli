package api

import (
	"context"
	"net/http"
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
	Name     *string            `json:"name,omitempty"`
	AppStore *AppStoreAppConfig `json:"app_store,omitempty"`
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
	PackageName string `json:"package_name"`
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

func (s *AppsService) List(ctx context.Context, projectID string) (*Page[App], error) {
	var out Page[App]
	err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "apps"), nil, &out)
	return &out, err
}

func (s *AppsService) Get(ctx context.Context, projectID, id string) (*App, error) {
	var out App
	err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "apps", id), nil, &out)
	return &out, err
}

func (s *AppsService) Create(ctx context.Context, projectID string, body AppCreate) (*App, error) {
	var out App
	err := s.c.do(ctx, http.MethodPost, encodePath("projects", projectID, "apps"), body, &out)
	return &out, err
}

func (s *AppsService) Update(ctx context.Context, projectID, id string, body AppUpdate) (*App, error) {
	var out App
	err := s.c.do(ctx, http.MethodPost, encodePath("projects", projectID, "apps", id), body, &out)
	return &out, err
}

func (s *AppsService) Delete(ctx context.Context, projectID, id string) error {
	return s.c.do(ctx, http.MethodDelete, encodePath("projects", projectID, "apps", id), nil, nil)
}

// GET /projects/{project_id}/apps/{app_id}/public_api_keys
//
// Shape unverified — fixture not captured (key would expose the project's
// public client API keys; not appropriate for a public fixture). Returns the
// raw response for callers to inspect.
func (s *AppsService) PublicAPIKeys(ctx context.Context, projectID, appID string) (any, error) {
	var out any
	err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "apps", appID, "public_api_keys"), nil, &out)
	return out, err
}

// GET /projects/{project_id}/apps/{app_id}/store_kit_config
func (s *AppsService) StoreKitConfig(ctx context.Context, projectID, appID string) (*StoreKitConfigFile, error) {
	var out StoreKitConfigFile
	err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "apps", appID, "store_kit_config"), nil, &out)
	return &out, err
}
