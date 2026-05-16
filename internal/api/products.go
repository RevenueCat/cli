package api

import (
	"context"
	"net/http"
)

type ProductsService struct{ c *Client }

type ProductSubscription struct {
	Duration            string `json:"duration,omitempty"` // ISO 8601 duration, e.g. "P1Y"
	GracePeriodDuration string `json:"grace_period_duration,omitempty"`
	TrialDuration       string `json:"trial_duration,omitempty"`
}

type Product struct {
	ID              string               `json:"id"`
	AppID           string               `json:"app_id,omitempty"`
	DisplayName     string               `json:"display_name,omitempty"`
	StoreIdentifier string               `json:"store_identifier,omitempty"`
	Type            string               `json:"type,omitempty"` // "subscription" | "one_time"
	State           string               `json:"state,omitempty"`
	Subscription    *ProductSubscription `json:"subscription,omitempty"`
	OneTime         any                  `json:"one_time,omitempty"`
	CreatedAt       Millis               `json:"created_at,omitempty"`
	Object          string               `json:"object,omitempty"`
}

func (s *ProductsService) List(ctx context.Context, projectID string) (*Page[Product], error) {
	var out Page[Product]
	err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "products"), nil, &out)
	return &out, err
}

func (s *ProductsService) Get(ctx context.Context, projectID, id string) (*Product, error) {
	var out Product
	err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "products", id), nil, &out)
	return &out, err
}

func (s *ProductsService) Delete(ctx context.Context, projectID, id string) error {
	return s.c.do(ctx, http.MethodDelete, encodePath("projects", projectID, "products", id), nil, nil)
}

func (s *ProductsService) Archive(ctx context.Context, projectID, id string) (*Product, error) {
	var out Product
	path := encodePath("projects", projectID, "products", id, "actions") + "/archive"
	err := s.c.do(ctx, http.MethodPost, path, nil, &out)
	return &out, err
}

func (s *ProductsService) Restore(ctx context.Context, projectID, id string) (*Product, error) {
	var out Product
	path := encodePath("projects", projectID, "products", id, "actions") + "/unarchive"
	err := s.c.do(ctx, http.MethodPost, path, nil, &out)
	return &out, err
}

// POST /projects/{project_id}/products/{id}/actions/push
//
// Push the product configuration to the underlying store.
func (s *ProductsService) Push(ctx context.Context, projectID, id string) error {
	path := encodePath("projects", projectID, "products", id, "actions") + "/push"
	return s.c.do(ctx, http.MethodPost, path, nil, nil)
}
