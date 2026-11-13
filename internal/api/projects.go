package api

import (
	"context"
	"net/http"
)

type ProjectsService struct{ c *Client }

// Project type is generated in types_gen.go.

// ListAll is GET /projects, draining every page.
func (s *ProjectsService) ListAll(ctx context.Context) (*Page[Project], error) {
	return listAll[Project](ctx, s.c, pathProjects(), 0)
}

// POST /projects
func (s *ProjectsService) Create(ctx context.Context, body ProjectCreate) (*Project, error) {
	var out Project
	if err := s.c.do(ctx, http.MethodPost, pathProjects(), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Get resolves a single project by ID. The v2 API doesn't expose a per-project
// GET endpoint yet, so this lists and filters. Callers don't need to know.
// Replace with a direct GET when it ships.
func (s *ProjectsService) Get(ctx context.Context, id string) (*Project, error) {
	page, err := s.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	for i := range page.Items {
		if page.Items[i].ID == id {
			return &page.Items[i], nil
		}
	}
	return nil, &APIError{Status: 404, Type: "resource_missing", Message: "project " + id + " not found"}
}
