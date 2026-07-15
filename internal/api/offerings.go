package api

import (
	"context"
	"net/http"
)

type OfferingsService struct{ c *Client }

// Offering type is generated in types_gen.go.

type OfferingCreate struct {
	LookupKey   string `json:"lookup_key"`
	DisplayName string `json:"display_name,omitempty"`
	Metadata    any    `json:"metadata,omitempty"`
}

type OfferingUpdate struct {
	DisplayName *string `json:"display_name,omitempty"`
	Metadata    any     `json:"metadata,omitempty"`
	IsCurrent   *bool   `json:"is_current,omitempty"`
}

func (s *OfferingsService) List(ctx context.Context, projectID string) (*Page[Offering], error) {
	var out Page[Offering]
	err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "offerings"), nil, &out)
	return &out, err
}

func (s *OfferingsService) Get(ctx context.Context, projectID, id string) (*Offering, error) {
	var out Offering
	err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "offerings", id), nil, &out)
	return &out, err
}

func (s *OfferingsService) Create(ctx context.Context, projectID string, body OfferingCreate) (*Offering, error) {
	var out Offering
	err := s.c.do(ctx, http.MethodPost, encodePath("projects", projectID, "offerings"), body, &out)
	return &out, err
}

func (s *OfferingsService) Update(ctx context.Context, projectID, id string, body OfferingUpdate) (*Offering, error) {
	var out Offering
	err := s.c.do(ctx, http.MethodPost, encodePath("projects", projectID, "offerings", id), body, &out)
	return &out, err
}

func (s *OfferingsService) Delete(ctx context.Context, projectID, id string) error {
	return s.c.do(ctx, http.MethodDelete, encodePath("projects", projectID, "offerings", id), nil, nil)
}

func (s *OfferingsService) Archive(ctx context.Context, projectID, id string) (*Offering, error) {
	var out Offering
	path := encodePath("projects", projectID, "offerings", id, "actions") + "/archive"
	err := s.c.do(ctx, http.MethodPost, path, nil, &out)
	return &out, err
}

func (s *OfferingsService) Restore(ctx context.Context, projectID, id string) (*Offering, error) {
	var out Offering
	path := encodePath("projects", projectID, "offerings", id, "actions") + "/unarchive"
	err := s.c.do(ctx, http.MethodPost, path, nil, &out)
	return &out, err
}
