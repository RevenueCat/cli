package api

import (
	"context"
	"net/http"
)

type PaywallsService struct{ c *Client }

type Paywall struct {
	ID                         string `json:"id"`
	Name                       string `json:"name,omitempty"`
	OfferingID                 string `json:"offering_id,omitempty"`
	AutomaticallyScaleFontSize bool   `json:"automatically_scale_font_size,omitempty"`
	CreatedAt                  Millis `json:"created_at,omitempty"`
	PublishedAt                Millis `json:"published_at,omitempty"`
	Object                     string `json:"object,omitempty"`
}

type PaywallCreate struct {
	OfferingID                 string `json:"offering_id"`
	AutomaticallyScaleFontSize bool   `json:"automatically_scale_font_size"`
}

func (s *PaywallsService) List(ctx context.Context, projectID string) (*Page[Paywall], error) {
	var out Page[Paywall]
	err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "paywalls"), nil, &out)
	return &out, err
}

func (s *PaywallsService) Get(ctx context.Context, projectID, id string) (*Paywall, error) {
	var out Paywall
	err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "paywalls", id), nil, &out)
	return &out, err
}

func (s *PaywallsService) Create(ctx context.Context, projectID string, body PaywallCreate) (*Paywall, error) {
	var out Paywall
	err := s.c.do(ctx, http.MethodPost, encodePath("projects", projectID, "paywalls"), body, &out)
	return &out, err
}

func (s *PaywallsService) Delete(ctx context.Context, projectID, id string) error {
	return s.c.do(ctx, http.MethodDelete, encodePath("projects", projectID, "paywalls", id), nil, nil)
}
