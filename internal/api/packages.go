package api

import (
	"context"
	"net/http"
)

type PackagesService struct{ c *Client }

type Package struct {
	ID          string `json:"id"`
	LookupKey   string `json:"lookup_key,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Position    *int   `json:"position,omitempty"`
	CreatedAt   Millis `json:"created_at,omitempty"`
	Object      string `json:"object,omitempty"`
}

func (s *PackagesService) List(ctx context.Context, projectID, offeringID string) (*Page[Package], error) {
	var out Page[Package]
	err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "offerings", offeringID, "packages"), nil, &out)
	return &out, err
}

func (s *PackagesService) Get(ctx context.Context, projectID, offeringID, id string) (*Package, error) {
	var out Package
	err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "offerings", offeringID, "packages", id), nil, &out)
	return &out, err
}

func (s *PackagesService) Delete(ctx context.Context, projectID, offeringID, id string) error {
	return s.c.do(ctx, http.MethodDelete, encodePath("projects", projectID, "offerings", offeringID, "packages", id), nil, nil)
}

type PackageCreate struct {
	LookupKey   string `json:"lookup_key"`
	DisplayName string `json:"display_name,omitempty"`
	Position    *int   `json:"position,omitempty"`
}

type PackageUpdate struct {
	DisplayName *string `json:"display_name,omitempty"`
	Position    *int    `json:"position,omitempty"`
}

func (s *PackagesService) Create(ctx context.Context, projectID, offeringID string, body PackageCreate) (*Package, error) {
	var out Package
	err := s.c.do(ctx, http.MethodPost, encodePath("projects", projectID, "offerings", offeringID, "packages"), body, &out)
	return &out, err
}

func (s *PackagesService) Update(ctx context.Context, projectID, offeringID, id string, body PackageUpdate) (*Package, error) {
	var out Package
	err := s.c.do(ctx, http.MethodPost, encodePath("projects", projectID, "offerings", offeringID, "packages", id), body, &out)
	return &out, err
}

func (s *PackagesService) ListProducts(ctx context.Context, projectID, offeringID, id string) (*Page[Product], error) {
	var out Page[Product]
	err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "offerings", offeringID, "packages", id, "products"), nil, &out)
	return &out, err
}

func (s *PackagesService) AttachProducts(ctx context.Context, projectID, offeringID, id string, productIDs []string) error {
	body := map[string]any{"product_ids": productIDs}
	path := encodePath("projects", projectID, "offerings", offeringID, "packages", id, "products") + "/attach"
	return s.c.do(ctx, http.MethodPost, path, body, nil)
}

func (s *PackagesService) DetachProducts(ctx context.Context, projectID, offeringID, id string, productIDs []string) error {
	body := map[string]any{"product_ids": productIDs}
	path := encodePath("projects", projectID, "offerings", offeringID, "packages", id, "products") + "/detach"
	return s.c.do(ctx, http.MethodPost, path, body, nil)
}
