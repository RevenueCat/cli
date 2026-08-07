package api

import (
	"context"
	"net/http"
)

type MetricsService struct{ c *Client }

// OverviewMetric is generated in types_gen.go.
// OverviewMetrics (the list wrapper) is also generated in types_gen.go.

// GET /projects/{project_id}/metrics/overview
//
// Not paginated; returns a fixed bundle of project-wide KPIs.
func (s *MetricsService) Overview(ctx context.Context, projectID string) (*OverviewMetrics, error) {
	var out OverviewMetrics
	err := s.c.do(ctx, http.MethodGet, pathMetricsOverview(projectID), nil, &out)
	return &out, err
}
