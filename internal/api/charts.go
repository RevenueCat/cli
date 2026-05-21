package api

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"time"
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
	Values            []ChartValue   `json:"values,omitempty"`
	Segments          any            `json:"segments,omitempty"`
	Summary           map[string]any `json:"summary,omitempty"`
	UserSelectors     map[string]any `json:"user_selectors,omitempty"`
	UnsupportedParams any            `json:"unsupported_params,omitempty"`
	Object            string         `json:"object,omitempty"`
	YAxis             string         `json:"yaxis,omitempty"`
	YAxisCurrency     string         `json:"yaxis_currency,omitempty"`
}

// ChartValue is one data point in the values array. Cohort is unix seconds.
type ChartValue struct {
	Cohort     int64   `json:"cohort"`
	Value      float64 `json:"value"`
	Measure    float64 `json:"measure"`
	Incomplete bool    `json:"incomplete"`
}

// ChartShowOptions controls what the chart endpoint returns.
type ChartShowOptions struct {
	// Resolution: "0"=day "1"=week "2"=month "3"=quarter "4"=year (empty = server default)
	Resolution string
	// StartDate / EndDate are unix seconds. Zero means no filter.
	StartDate int64
	EndDate   int64
	// Filters are chart-specific key=value params (from --filter flags).
	Filters map[string]string
}

func (s *ChartsService) Show(ctx context.Context, projectID, name string, opts ChartShowOptions) (*ChartData, error) {
	path := encodePath("projects", projectID, "charts", name)
	q := url.Values{}
	if opts.Resolution != "" {
		q.Set("resolution", opts.Resolution)
	}
	if opts.StartDate != 0 {
		q.Set("start_date", time.Unix(opts.StartDate, 0).UTC().Format("2006-01-02"))
	}
	if opts.EndDate != 0 {
		q.Set("end_date", time.Unix(opts.EndDate, 0).UTC().Format("2006-01-02"))
	}
	for k, v := range opts.Filters {
		q.Set(k, v)
	}
	if len(q) > 0 {
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
