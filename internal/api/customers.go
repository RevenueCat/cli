package api

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

type CustomersService struct{ c *Client }

// Customer, Entitlement types are generated in types_gen.go.

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
	if err := s.c.do(ctx, http.MethodGet, pathCustomers(projectID)+opts.query(), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GET /projects/{project_id}/customers/{customer_id}
func (s *CustomersService) Get(ctx context.Context, projectID, customerID string) (*Customer, error) {
	var out Customer
	if err := s.c.do(ctx, http.MethodGet, pathCustomer(projectID, customerID), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GET /projects/{project_id}/customers/{customer_id}/subscriptions
func (s *CustomersService) Subscriptions(ctx context.Context, projectID, customerID string) (*Page[Subscription], error) {
	var out Page[Subscription]
	if err := s.c.do(ctx, http.MethodGet, pathCustomerSubscriptions(projectID, customerID), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GET /projects/{project_id}/customers/{customer_id}/purchases
func (s *CustomersService) Purchases(ctx context.Context, projectID, customerID string) (*Page[Purchase], error) {
	var out Page[Purchase]
	if err := s.c.do(ctx, http.MethodGet, pathCustomerPurchases(projectID, customerID), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GET /projects/{project_id}/customers/{customer_id}/active_entitlements
//
// NOTE: also embedded in the customer GET response.
func (s *CustomersService) ActiveEntitlements(ctx context.Context, projectID, customerID string) (*Page[CustomerEntitlement], error) {
	var out Page[CustomerEntitlement]
	if err := s.c.do(ctx, http.MethodGet, pathCustomerActiveEntitlements(projectID, customerID), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// POST /projects/{project_id}/customers/{customer_id}/actions/grant_entitlement
//
// The v2 endpoint takes expires_at (ms since epoch), not a named duration, and
// returns the full Customer (with the granted entitlement embedded), not a bare
// Entitlement. Callers translate a friendly duration into expiresAt.
func (s *CustomersService) GrantEntitlement(ctx context.Context, projectID, customerID, entitlementID string, expiresAt int64) (*Customer, error) {
	body := map[string]any{"entitlement_id": entitlementID, "expires_at": expiresAt}
	var out Customer
	path := pathCustomerActionsGrantEntitlement(projectID, customerID)
	if err := s.c.do(ctx, http.MethodPost, path, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// POST /projects/{project_id}/customers/{customer_id}/actions/revoke_granted_entitlement
func (s *CustomersService) RevokeEntitlement(ctx context.Context, projectID, customerID, entitlementID string) error {
	body := map[string]any{"entitlement_id": entitlementID}
	path := pathCustomerActionsRevokeGrantedEntitlement(projectID, customerID)
	return s.c.do(ctx, http.MethodPost, path, body, nil)
}

// GET /projects/{project_id}/customers/{customer_id}/aliases
func (s *CustomersService) Aliases(ctx context.Context, projectID, customerID string) (*Page[map[string]any], error) {
	var out Page[map[string]any]
	err := s.c.do(ctx, http.MethodGet, pathCustomerAliases(projectID, customerID), nil, &out)
	return &out, err
}

// GET /projects/{project_id}/customers/{customer_id}/attributes
//
// Returns a Page envelope of {name, value, updated_at} objects (verified live);
// not a flat key→value map as one might guess from the name.
func (s *CustomersService) Attributes(ctx context.Context, projectID, customerID string) (*Page[map[string]any], error) {
	var out Page[map[string]any]
	err := s.c.do(ctx, http.MethodGet, pathCustomerAttributes(projectID, customerID), nil, &out)
	return &out, err
}

// POST /projects/{project_id}/customers/{customer_id}/attributes
//
// The API expects {"attributes": [{name, value}, ...]} per the OpenAPI spec.
func (s *CustomersService) SetAttributes(ctx context.Context, projectID, customerID string, attrs map[string]string) error {
	type attrItem struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	items := make([]attrItem, 0, len(attrs))
	for k, v := range attrs {
		items = append(items, attrItem{Name: k, Value: v})
	}
	body := map[string]any{"attributes": items}
	return s.c.do(ctx, http.MethodPost, pathCustomerAttributes(projectID, customerID), body, nil)
}

// POST /projects/{project_id}/customers/{customer_id}/actions/transfer
func (s *CustomersService) Transfer(ctx context.Context, projectID, sourceCustomerID, destCustomerID string) error {
	body := map[string]any{"destination_customer_id": destCustomerID}
	path := pathCustomerActionsTransfer(projectID, sourceCustomerID)
	return s.c.do(ctx, http.MethodPost, path, body, nil)
}

// POST /projects/{project_id}/customers/{customer_id}/actions/assign_offering
//
// Pass empty offeringID to clear the override (sends null per spec).
func (s *CustomersService) OverrideOffering(ctx context.Context, projectID, customerID, offeringID string) error {
	var id *string
	if offeringID != "" {
		id = &offeringID
	}
	body := map[string]any{"offering_id": id}
	path := pathCustomerActionsAssignOffering(projectID, customerID)
	return s.c.do(ctx, http.MethodPost, path, body, nil)
}

// POST /projects/{project_id}/customers/{customer_id}/actions/restore_purchase_by_order_id
func (s *CustomersService) RestoreGooglePlay(ctx context.Context, projectID, customerID, token string) error {
	body := map[string]any{"fetch_token": token}
	path := pathCustomerActionsRestorePurchaseByOrderID(projectID, customerID)
	return s.c.do(ctx, http.MethodPost, path, body, nil)
}

// GET /projects/{project_id}/customers/{customer_id}/virtual_currencies
func (s *CustomersService) Wallet(ctx context.Context, projectID, customerID string) (*Page[map[string]any], error) {
	var out Page[map[string]any]
	err := s.c.do(ctx, http.MethodGet, pathCustomerVirtualCurrencies(projectID, customerID), nil, &out)
	return &out, err
}

// POST /projects/{project_id}/customers/{customer_id}/virtual_currencies/update_balance
func (s *CustomersService) WalletAdjustBalance(ctx context.Context, projectID, customerID, currencyCode string, amount int64) error {
	body := map[string]any{"currency_code": currencyCode, "amount": amount}
	return s.c.do(ctx, http.MethodPost, pathCustomerVirtualCurrenciesUpdateBalance(projectID, customerID), body, nil)
}

// POST /projects/{project_id}/customers/{customer_id}/virtual_currencies/transactions
func (s *CustomersService) WalletTransaction(ctx context.Context, projectID, customerID, currencyCode string, amount int64) error {
	body := map[string]any{"currency_code": currencyCode, "amount": amount}
	return s.c.do(ctx, http.MethodPost, pathCustomerVirtualCurrenciesTransactions(projectID, customerID), body, nil)
}
