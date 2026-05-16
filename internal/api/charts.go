package api

import (
	"context"
	"net/http"
	"net/url"
	"sort"
)

type ChartsService struct{ c *Client }

// ValidChartNames is the closed enum the API enforces server-side. Discovered
// by triggering a parameter_error: any name outside this list returns 400.
// Kept here so we can validate locally and offer shell completion.
var ValidChartNames = []string{
	"actives", "actives_movement", "actives_new", "arr", "churn",
	"cohort_explorer", "conversion_to_paying", "customers_active",
	"customers_new", "ltv_per_customer", "ltv_per_paying_customer",
	"mrr", "mrr_movement", "prediction_explorer", "refund_rate", "revenue",
	"subscription_retention", "subscription_status", "trial_conversion_rate",
	"trials", "trials_movement", "trials_new",
}

func IsValidChartName(name string) bool {
	idx := sort.SearchStrings(ValidChartNames, name)
	return idx < len(ValidChartNames) && ValidChartNames[idx] == name
}

// ChartData is what GET /charts/{name} returns. Most fields are pass-through:
// the chart shape is highly variable per chart_name and we don't want to lose
// fields by over-modeling.
type ChartData struct {
	DisplayName       string         `json:"display_name,omitempty"`
	Category          string         `json:"category,omitempty"`
	Description       string         `json:"description,omitempty"`
	Resolution        string         `json:"resolution,omitempty"`
	StartDate         int64          `json:"start_date,omitempty"` // unix SECONDS (not millis — note inconsistency)
	EndDate           int64          `json:"end_date,omitempty"`
	LastComputedAt    any            `json:"last_computed_at,omitempty"`
	Measures          []any          `json:"measures,omitempty"`
	Values            []any          `json:"values,omitempty"`
	Segments          any            `json:"segments,omitempty"`
	Summary           map[string]any `json:"summary,omitempty"`
	UserSelectors     map[string]any `json:"user_selectors,omitempty"`
	UnsupportedParams any            `json:"unsupported_params,omitempty"`
	Object            string         `json:"object,omitempty"`
}

func (s *ChartsService) Show(ctx context.Context, projectID, name string, filters map[string]string) (*ChartData, error) {
	path := encodePath("projects", projectID, "charts", name)
	if len(filters) > 0 {
		q := url.Values{}
		for k, v := range filters {
			q.Set(k, v)
		}
		path += "?" + q.Encode()
	}
	var out ChartData
	err := s.c.do(ctx, http.MethodGet, path, nil, &out)
	return &out, err
}

func (s *ChartsService) Options(ctx context.Context, projectID, name string) (map[string]any, error) {
	var out map[string]any
	err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "charts", name, "options"), nil, &out)
	return out, err
}

func init() {
	sort.Strings(ValidChartNames)
}
