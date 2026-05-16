package api

import (
	"context"
	"net/http"
)

type PurchasesService struct{ c *Client }

type Purchase struct {
	ID          string `json:"id"`
	CustomerID  string `json:"customer_id,omitempty"`
	ProductID   string `json:"product_id,omitempty"`
	Store       string `json:"store,omitempty"`
	PurchasedAt Millis `json:"purchased_at,omitempty"`
	Object      string `json:"object,omitempty"`
}

// GET /projects/{project_id}/purchases/{id}
func (s *PurchasesService) Get(ctx context.Context, projectID, id string) (*Purchase, error) {
	var out Purchase
	err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "purchases", id), nil, &out)
	return &out, err
}

// GET /projects/{project_id}/purchases/{id}/entitlements
func (s *PurchasesService) Entitlements(ctx context.Context, projectID, id string) (*Page[Entitlement], error) {
	var out Page[Entitlement]
	err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "purchases", id, "entitlements"), nil, &out)
	return &out, err
}

// POST /projects/{project_id}/purchases/{id}/actions/refund
// Web Billing purchases only.
func (s *PurchasesService) Refund(ctx context.Context, projectID, id string) error {
	path := encodePath("projects", projectID, "purchases", id, "actions") + "/refund"
	return s.c.do(ctx, http.MethodPost, path, nil, nil)
}
