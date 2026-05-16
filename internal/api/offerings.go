package api

import (
	"context"
	"net/http"
)

type OfferingsService struct{ c *Client }

type Offering struct {
	ID          string `json:"id"`
	LookupKey   string `json:"lookup_key,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	IsCurrent   bool   `json:"is_current,omitempty"`
	State       string `json:"state,omitempty"`
	Metadata    any    `json:"metadata,omitempty"`
	ProjectID   string `json:"project_id,omitempty"`
	CreatedAt   Millis `json:"created_at,omitempty"`
	Object      string `json:"object,omitempty"`
}

type OfferingCreate struct {
	LookupKey   string `json:"lookup_key"`
	DisplayName string `json:"display_name,omitempty"`
	Metadata    any    `json:"metadata,omitempty"`
}

type OfferingUpdate struct {
	DisplayName *string `json:"display_name,omitempty"`
	Metadata    any     `json:"metadata,omitempty"`
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
