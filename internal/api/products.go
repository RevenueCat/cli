package api

import (
	"context"
	"net/http"
	"net/url"
)

type ProductsService struct{ c *Client }

// Product type is generated in types_gen.go.

type ProductListOptions struct {
	AppID string
}

type ProductCreate struct {
	StoreIdentifier string `json:"store_identifier"`
	Type            string `json:"type"` // "subscription" | "one_time"
	AppID           string `json:"app_id"`
	DisplayName     string `json:"display_name,omitempty"`
}

type ProductUpdate struct {
	DisplayName *string `json:"display_name,omitempty"`
}

func (s *ProductsService) List(ctx context.Context, projectID string, opts *ProductListOptions) (*Page[Product], error) {
	path := encodePath("projects", projectID, "products")
	if opts != nil && opts.AppID != "" {
		path += "?app_id=" + url.QueryEscape(opts.AppID)
	}
	var out Page[Product]
	err := s.c.do(ctx, http.MethodGet, path, nil, &out)
	return &out, err
}

func (s *ProductsService) Create(ctx context.Context, projectID string, body ProductCreate) (*Product, error) {
	var out Product
	err := s.c.do(ctx, http.MethodPost, encodePath("projects", projectID, "products"), body, &out)
	return &out, err
}

func (s *ProductsService) Get(ctx context.Context, projectID, id string) (*Product, error) {
	var out Product
	err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "products", id), nil, &out)
	return &out, err
}

func (s *ProductsService) Update(ctx context.Context, projectID, id string, body ProductUpdate) (*Product, error) {
	var out Product
	err := s.c.do(ctx, http.MethodPost, encodePath("projects", projectID, "products", id), body, &out)
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

// POST /projects/{project_id}/products/{id}/create_in_store
//
// Push the product configuration to the underlying store.
func (s *ProductsService) Push(ctx context.Context, projectID, id string) error {
	path := encodePath("projects", projectID, "products", id) + "/create_in_store"
	return s.c.do(ctx, http.MethodPost, path, nil, nil)
}
