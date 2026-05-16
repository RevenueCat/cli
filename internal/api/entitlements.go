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
	if err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "entitlements"), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GET /projects/{project_id}/entitlements/{id}
func (s *EntitlementsService) Get(ctx context.Context, projectID, id string) (*Entitlement, error) {
	var out Entitlement
	if err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "entitlements", id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// POST /projects/{project_id}/entitlements
func (s *EntitlementsService) Create(ctx context.Context, projectID string, body EntitlementCreate) (*Entitlement, error) {
	var out Entitlement
	if err := s.c.do(ctx, http.MethodPost, encodePath("projects", projectID, "entitlements"), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// POST /projects/{project_id}/entitlements/{id}
func (s *EntitlementsService) Update(ctx context.Context, projectID, id string, body EntitlementUpdate) (*Entitlement, error) {
	var out Entitlement
	if err := s.c.do(ctx, http.MethodPost, encodePath("projects", projectID, "entitlements", id), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DELETE /projects/{project_id}/entitlements/{id}
func (s *EntitlementsService) Delete(ctx context.Context, projectID, id string) error {
	return s.c.do(ctx, http.MethodDelete, encodePath("projects", projectID, "entitlements", id), nil, nil)
}
