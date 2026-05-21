package api

import (
	"context"
	"net/http"
)

type PackagesService struct{ c *Client }

// Package type is generated in types_gen.go.

func (s *PackagesService) List(ctx context.Context, projectID, offeringID string) (*Page[Package], error) {
	var out Page[Package]
	err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "offerings", offeringID, "packages"), nil, &out)
	return &out, err
}

func (s *PackagesService) Get(ctx context.Context, projectID, id string) (*Package, error) {
	var out Package
	err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "packages", id), nil, &out)
	return &out, err
}

func (s *PackagesService) Delete(ctx context.Context, projectID, id string) error {
	return s.c.do(ctx, http.MethodDelete, encodePath("projects", projectID, "packages", id), nil, nil)
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

func (s *PackagesService) Update(ctx context.Context, projectID, id string, body PackageUpdate) (*Package, error) {
	var out Package
	err := s.c.do(ctx, http.MethodPost, encodePath("projects", projectID, "packages", id), body, &out)
	return &out, err
}

func (s *PackagesService) ListProducts(ctx context.Context, projectID, id string) (*Page[Product], error) {
	var out Page[Product]
	err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "packages", id, "products"), nil, &out)
	return &out, err
}

func (s *PackagesService) AttachProducts(ctx context.Context, projectID, id string, productIDs []string) error {
	body := map[string]any{"product_ids": productIDs}
	path := encodePath("projects", projectID, "packages", id, "actions") + "/attach_products"
	return s.c.do(ctx, http.MethodPost, path, body, nil)
}

func (s *PackagesService) DetachProducts(ctx context.Context, projectID, id string, productIDs []string) error {
	body := map[string]any{"product_ids": productIDs}
	path := encodePath("projects", projectID, "packages", id, "actions") + "/detach_products"
	return s.c.do(ctx, http.MethodPost, path, body, nil)
}
