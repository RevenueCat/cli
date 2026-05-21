package api

import (
	"context"
	"net/http"
)

type AppsService struct{ c *Client }

// AppCreate and AppUpdate are request bodies used by Create/Update methods.
// The App, AppType etc. response types are generated in types_gen.go.

type AppCreate struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type AppUpdate struct {
	Name *string `json:"name,omitempty"`
}

func (s *AppsService) List(ctx context.Context, projectID string) (*Page[App], error) {
	var out Page[App]
	err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "apps"), nil, &out)
	return &out, err
}

func (s *AppsService) Get(ctx context.Context, projectID, id string) (*App, error) {
	var out App
	err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "apps", id), nil, &out)
	return &out, err
}

func (s *AppsService) Create(ctx context.Context, projectID string, body AppCreate) (*App, error) {
	var out App
	err := s.c.do(ctx, http.MethodPost, encodePath("projects", projectID, "apps"), body, &out)
	return &out, err
}

func (s *AppsService) Update(ctx context.Context, projectID, id string, body AppUpdate) (*App, error) {
	var out App
	err := s.c.do(ctx, http.MethodPost, encodePath("projects", projectID, "apps", id), body, &out)
	return &out, err
}

func (s *AppsService) Delete(ctx context.Context, projectID, id string) error {
	return s.c.do(ctx, http.MethodDelete, encodePath("projects", projectID, "apps", id), nil, nil)
}

// GET /projects/{project_id}/apps/{app_id}/public_api_keys
//
// Shape unverified — fixture not captured (key would expose the project's
// public client API keys; not appropriate for a public fixture). Returns the
// raw response for callers to inspect.
func (s *AppsService) PublicAPIKeys(ctx context.Context, projectID, appID string) (any, error) {
	var out any
	err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "apps", appID, "public_api_keys"), nil, &out)
	return out, err
}
