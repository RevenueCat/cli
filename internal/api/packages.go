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
