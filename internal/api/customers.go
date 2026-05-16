package api

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

type CustomersService struct{ c *Client }

// Customer shape verified against real GET /v2/projects/.../customers responses.
// The single-customer GET also embeds active_entitlements as a Page[Entitlement].
type Customer struct {
	ID                      string             `json:"id"`
	ProjectID               string             `json:"project_id,omitempty"`
	FirstSeenAt             Millis             `json:"first_seen_at,omitempty"`
	LastSeenAt              Millis             `json:"last_seen_at,omitempty"`
	LastSeenAppVersion      string             `json:"last_seen_app_version,omitempty"`
	LastSeenCountry         string             `json:"last_seen_country,omitempty"`
	LastSeenPlatform        string             `json:"last_seen_platform,omitempty"`
	LastSeenPlatformVersion string             `json:"last_seen_platform_version,omitempty"`
	Experiment              any                `json:"experiment,omitempty"`
	Object                  string             `json:"object,omitempty"`
	ActiveEntitlements      *Page[Entitlement] `json:"active_entitlements,omitempty"`
}

type Entitlement struct {
	ID          string `json:"id"`
	LookupKey   string `json:"lookup_key,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	ProjectID   string `json:"project_id,omitempty"`
	CreatedAt   Millis `json:"created_at,omitempty"`
	ExpiresAt   Millis `json:"expires_at,omitempty"`
	GrantedAt   Millis `json:"granted_at,omitempty"`
	Source      string `json:"source,omitempty"`
	Object      string `json:"object,omitempty"`
}

type Subscription struct {
	ID                string `json:"id"`
	CustomerID        string `json:"customer_id,omitempty"`
	ProductID         string `json:"product_id,omitempty"`
	Store             string `json:"store,omitempty"`
	StartsAt          Millis `json:"starts_at,omitempty"`
	CurrentPeriodEnds Millis `json:"current_period_ends_at,omitempty"`
	GivesAccess       bool   `json:"gives_access,omitempty"`
	AutoRenewalStatus string `json:"auto_renewal_status,omitempty"`
	Status            string `json:"status,omitempty"`
	Object            string `json:"object,omitempty"`
}

type Purchase struct {
	ID          string `json:"id"`
	CustomerID  string `json:"customer_id,omitempty"`
	ProductID   string `json:"product_id,omitempty"`
	Store       string `json:"store,omitempty"`
	PurchasedAt Millis `json:"purchased_at,omitempty"`
	Object      string `json:"object,omitempty"`
}

type ListCustomersOptions struct {
	Limit         int    // default 0 → server default
	StartingAfter string // cursor (raw customer ID, not the full URL)
}

func (o *ListCustomersOptions) query() string {
	if o == nil {
		return ""
	}
	v := url.Values{}
	if o.Limit > 0 {
		v.Set("limit", strconv.Itoa(o.Limit))
	}
	if o.StartingAfter != "" {
		v.Set("starting_after", o.StartingAfter)
	}
	if len(v) == 0 {
		return ""
	}
	return "?" + v.Encode()
}

// GET /projects/{project_id}/customers
func (s *CustomersService) List(ctx context.Context, projectID string, opts *ListCustomersOptions) (*Page[Customer], error) {
	var out Page[Customer]
	if err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "customers")+opts.query(), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GET /projects/{project_id}/customers/{customer_id}
func (s *CustomersService) Get(ctx context.Context, projectID, customerID string) (*Customer, error) {
	var out Customer
	if err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "customers", customerID), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GET /projects/{project_id}/customers/{customer_id}/subscriptions
func (s *CustomersService) Subscriptions(ctx context.Context, projectID, customerID string) (*Page[Subscription], error) {
	var out Page[Subscription]
	if err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "customers", customerID, "subscriptions"), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GET /projects/{project_id}/customers/{customer_id}/purchases
func (s *CustomersService) Purchases(ctx context.Context, projectID, customerID string) (*Page[Purchase], error) {
	var out Page[Purchase]
	if err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "customers", customerID, "purchases"), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GET /projects/{project_id}/customers/{customer_id}/active_entitlements
//
// NOTE: also embedded in the customer GET response.
func (s *CustomersService) ActiveEntitlements(ctx context.Context, projectID, customerID string) (*Page[Entitlement], error) {
	var out Page[Entitlement]
	if err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "customers", customerID, "active_entitlements"), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// POST /projects/{project_id}/customers/{customer_id}/actions/grant_entitlement
func (s *CustomersService) GrantEntitlement(ctx context.Context, projectID, customerID, entitlementID, duration string) (*Entitlement, error) {
	body := map[string]any{"entitlement_id": entitlementID, "duration": duration}
	var out Entitlement
	path := encodePath("projects", projectID, "customers", customerID, "actions") + "/grant_entitlement"
	if err := s.c.do(ctx, http.MethodPost, path, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// POST /projects/{project_id}/customers/{customer_id}/actions/revoke_entitlement
func (s *CustomersService) RevokeEntitlement(ctx context.Context, projectID, customerID, entitlementID string) error {
	body := map[string]any{"entitlement_id": entitlementID}
	path := encodePath("projects", projectID, "customers", customerID, "actions") + "/revoke_entitlement"
	return s.c.do(ctx, http.MethodPost, path, body, nil)
}
