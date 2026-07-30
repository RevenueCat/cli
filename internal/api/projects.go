package api

import (
	"context"
	"net/http"
	"net/url"
)

type ProjectsService struct{ c *Client }

// Project type is generated in types_gen.go.

// GET /projects, following next_page until the account's projects are drained.
// next_page is a full URL; we re-request through the base-relative path using
// its starting_after cursor. If a page reports no cursor, we stop with what we
// have (no worse than a single-page fetch).
func (s *ProjectsService) List(ctx context.Context) (*Page[Project], error) {
	var all []Project
	path := "/projects"
	for {
		var page Page[Project]
		if err := s.c.do(ctx, http.MethodGet, path, nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Items...)
		if page.NextPage == "" {
			break
		}
		u, err := url.Parse(page.NextPage)
		if err != nil {
			break
		}
		cursor := u.Query().Get("starting_after")
		if cursor == "" {
			break
		}
		path = "/projects?starting_after=" + url.QueryEscape(cursor)
	}
	return &Page[Project]{Items: all}, nil
}

// POST /projects
func (s *ProjectsService) Create(ctx context.Context, body ProjectCreate) (*Project, error) {
	var out Project
	if err := s.c.do(ctx, http.MethodPost, "/projects", body, &out); err != nil {
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
	return nil, &APIError{Status: 404, Type: "resource_missing", Message: "project " + id + " not found"}
}
