package api

import (
	"context"
	"net/http"
)

type BenchmarksService struct{ c *Client }

// BenchmarksResponse is the project-level benchmarks bundle. Loosely typed —
// the live fixture returned an empty `metrics` list, so the per-metric shape
// is documented but not modeled yet.
type BenchmarksResponse struct {
	Metrics []map[string]any `json:"metrics"`
	Object  string           `json:"object,omitempty"`
}

func (s *BenchmarksService) Get(ctx context.Context, projectID string) (*BenchmarksResponse, error) {
	var out BenchmarksResponse
	err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "benchmarks"), nil, &out)
	return &out, err
}
