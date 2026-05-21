package api

import (
	"context"
	"net/http"
)

type SubscriptionsService struct{ c *Client }

// Subscription type is generated in types_gen.go.
// Transaction and ManagementURL are hand-written below (not in spec).

type Transaction struct {
	ID             string `json:"id"`
	SubscriptionID string `json:"subscription_id,omitempty"`
	PurchasedAt    int64  `json:"purchased_at,omitempty"`
	RevenueInUSD   any    `json:"revenue_in_usd,omitempty"`
	Object         string `json:"object,omitempty"`
}

// ManagementURL is returned by GET subscriptions/{id}/management_url. The
// endpoint emits a tiny object rather than a bare string.
type ManagementURL struct {
	URL    string `json:"url"`
	Object string `json:"object,omitempty"`
}

// GET /projects/{project_id}/subscriptions/{id}
func (s *SubscriptionsService) Get(ctx context.Context, projectID, id string) (*Subscription, error) {
	var out Subscription
	err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "subscriptions", id), nil, &out)
	return &out, err
}

// GET /projects/{project_id}/subscriptions/{id}/transactions
func (s *SubscriptionsService) Transactions(ctx context.Context, projectID, id string) (*Page[Transaction], error) {
	var out Page[Transaction]
	err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "subscriptions", id, "transactions"), nil, &out)
	return &out, err
}

// GET /projects/{project_id}/subscriptions/{id}/entitlements
func (s *SubscriptionsService) Entitlements(ctx context.Context, projectID, id string) (*Page[Entitlement], error) {
	var out Page[Entitlement]
	err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "subscriptions", id, "entitlements"), nil, &out)
	return &out, err
}

// GET /projects/{project_id}/subscriptions/{id}/management_url
func (s *SubscriptionsService) ManagementURL(ctx context.Context, projectID, id string) (*ManagementURL, error) {
	var out ManagementURL
	err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "subscriptions", id, "management_url"), nil, &out)
	return &out, err
}

// POST /projects/{project_id}/subscriptions/{id}/actions/cancel
// Web Billing subscriptions only.
func (s *SubscriptionsService) Cancel(ctx context.Context, projectID, id string) (*Subscription, error) {
	var out Subscription
	path := encodePath("projects", projectID, "subscriptions", id, "actions") + "/cancel"
	err := s.c.do(ctx, http.MethodPost, path, nil, &out)
	return &out, err
}

// POST /projects/{project_id}/subscriptions/{id}/actions/extend
// duration is ISO 8601 (e.g. "P1M", "P7D").
func (s *SubscriptionsService) Extend(ctx context.Context, projectID, id, duration string) (*Subscription, error) {
	var out Subscription
	path := encodePath("projects", projectID, "subscriptions", id, "actions") + "/extend"
	err := s.c.do(ctx, http.MethodPost, path, map[string]any{"duration": duration}, &out)
	return &out, err
}

// POST /projects/{project_id}/subscriptions/{id}/actions/refund
// Web Billing subscriptions only.
func (s *SubscriptionsService) Refund(ctx context.Context, projectID, id string) error {
	path := encodePath("projects", projectID, "subscriptions", id, "actions") + "/refund"
	return s.c.do(ctx, http.MethodPost, path, nil, nil)
}
