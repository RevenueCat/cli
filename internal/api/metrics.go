package api

import (
	"context"
	"net/http"
)

type MetricsService struct{ c *Client }

type OverviewMetric struct {
	ID                 string `json:"id"`
	Name               string `json:"name,omitempty"`
	Description        string `json:"description,omitempty"`
	Unit               string `json:"unit,omitempty"`
	Period             string `json:"period,omitempty"`
	Value              any    `json:"value,omitempty"`
	LastUpdatedAt      any    `json:"last_updated_at,omitempty"`
	LastUpdatedISO8601 string `json:"last_updated_at_iso8601,omitempty"`
	Object             string `json:"object,omitempty"`
}

type OverviewResponse struct {
	Metrics []OverviewMetric `json:"metrics"`
	Object  string           `json:"object,omitempty"`
}

// GET /projects/{project_id}/metrics/overview
//
// Not paginated; returns a fixed bundle of project-wide KPIs.
func (s *MetricsService) Overview(ctx context.Context, projectID string) (*OverviewResponse, error) {
	var out OverviewResponse
	err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "metrics", "overview"), nil, &out)
	return &out, err
}
