package api

import (
	"context"
	"net/http"
)

type ProjectsService struct{ c *Client }

// Project shape verified against real GET /v2/projects responses.
type Project struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	CreatedAt    Millis `json:"created_at"`
	IconURL      string `json:"icon_url,omitempty"`
	IconURLLarge string `json:"icon_url_large,omitempty"`
	Object       string `json:"object,omitempty"`
}

// GET /projects
func (s *ProjectsService) List(ctx context.Context) (*Page[Project], error) {
	var out Page[Project]
	if err := s.c.do(ctx, http.MethodGet, "/projects", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Get resolves a single project by ID. The v2 API doesn't expose a per-project
// GET endpoint yet, so this lists and filters. Callers don't need to know.
// Replace with a direct GET when it ships.
func (s *ProjectsService) Get(ctx context.Context, id string) (*Project, error) {
	page, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range page.Items {
		if page.Items[i].ID == id {
			return &page.Items[i], nil
		}
	}
	return nil, &Error{Status: 404, Type: "resource_missing", Message: "project " + id + " not found"}
}
