package api

import (
	"context"
	"net/http"
)

type EntitlementsService struct{ c *Client }

// Note: Entitlement struct lives in customers.go because it's shared with the
// customer-embedded active_entitlements view.

type EntitlementCreate struct {
	LookupKey   string `json:"lookup_key"`
	DisplayName string `json:"display_name,omitempty"`
}

type EntitlementUpdate struct {
	DisplayName *string `json:"display_name,omitempty"`
}

// GET /projects/{project_id}/entitlements
func (s *EntitlementsService) List(ctx context.Context, projectID string) (*Page[Entitlement], error) {
	var out Page[Entitlement]
	if err := s.c.do(ctx, http.MethodGet, pathEntitlements(projectID), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GET /projects/{project_id}/entitlements/{id}
func (s *EntitlementsService) Get(ctx context.Context, projectID, id string) (*Entitlement, error) {
	var out Entitlement
	if err := s.c.do(ctx, http.MethodGet, pathEntitlement(projectID, id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// POST /projects/{project_id}/entitlements
func (s *EntitlementsService) Create(ctx context.Context, projectID string, body EntitlementCreate) (*Entitlement, error) {
	var out Entitlement
	if err := s.c.do(ctx, http.MethodPost, pathEntitlements(projectID), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// POST /projects/{project_id}/entitlements/{id}
func (s *EntitlementsService) Update(ctx context.Context, projectID, id string, body EntitlementUpdate) (*Entitlement, error) {
	var out Entitlement
	if err := s.c.do(ctx, http.MethodPost, pathEntitlement(projectID, id), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DELETE /projects/{project_id}/entitlements/{id}
func (s *EntitlementsService) Delete(ctx context.Context, projectID, id string) error {
	return s.c.do(ctx, http.MethodDelete, pathEntitlement(projectID, id), nil, nil)
}

// POST /projects/{project_id}/entitlements/{id}/actions/archive
func (s *EntitlementsService) Archive(ctx context.Context, projectID, id string) (*Entitlement, error) {
	var out Entitlement
	path := pathEntitlementActionsArchive(projectID, id)
	err := s.c.do(ctx, http.MethodPost, path, nil, &out)
	return &out, err
}

// POST /projects/{project_id}/entitlements/{id}/actions/unarchive
func (s *EntitlementsService) Restore(ctx context.Context, projectID, id string) (*Entitlement, error) {
	var out Entitlement
	path := pathEntitlementActionsUnarchive(projectID, id)
	err := s.c.do(ctx, http.MethodPost, path, nil, &out)
	return &out, err
}

// GET /projects/{project_id}/entitlements/{id}/products
func (s *EntitlementsService) ListProducts(ctx context.Context, projectID, id string) (*Page[Product], error) {
	var out Page[Product]
	err := s.c.do(ctx, http.MethodGet, pathEntitlementProducts(projectID, id), nil, &out)
	return &out, err
}

// POST /projects/{project_id}/entitlements/{id}/actions/attach_products
func (s *EntitlementsService) AttachProducts(ctx context.Context, projectID, id string, productIDs []string) error {
	body := map[string]any{"product_ids": productIDs}
	path := pathEntitlementActionsAttachProducts(projectID, id)
	return s.c.do(ctx, http.MethodPost, path, body, nil)
}

// POST /projects/{project_id}/entitlements/{id}/actions/detach_products
func (s *EntitlementsService) DetachProducts(ctx context.Context, projectID, id string, productIDs []string) error {
	body := map[string]any{"product_ids": productIDs}
	path := pathEntitlementActionsDetachProducts(projectID, id)
	return s.c.do(ctx, http.MethodPost, path, body, nil)
}
