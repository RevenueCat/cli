package api

import (
	"context"
	"net/http"
)

type PurchasesService struct{ c *Client }

// Purchase type is generated in types_gen.go.

// GET /projects/{project_id}/purchases/{id}
func (s *PurchasesService) Get(ctx context.Context, projectID, id string) (*Purchase, error) {
	var out Purchase
	err := s.c.do(ctx, http.MethodGet, pathPurchase(projectID, id), nil, &out)
	return &out, err
}

// GET /projects/{project_id}/purchases/{id}/entitlements
func (s *PurchasesService) Entitlements(ctx context.Context, projectID, id string) (*Page[Entitlement], error) {
	var out Page[Entitlement]
	err := s.c.do(ctx, http.MethodGet, pathPurchaseEntitlements(projectID, id), nil, &out)
	return &out, err
}

// POST /projects/{project_id}/purchases/{id}/actions/refund
// Web Billing purchases only.
func (s *PurchasesService) Refund(ctx context.Context, projectID, id string) error {
	path := pathPurchaseActionsRefund(projectID, id)
	return s.c.do(ctx, http.MethodPost, path, nil, nil)
}
