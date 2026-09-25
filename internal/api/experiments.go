package api

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

type ExperimentsService struct{ c *Client }

type ExperimentOffering struct {
	ID          string `json:"id"`
	Identifier  string `json:"identifier,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	PaywallID   string `json:"paywall_id,omitempty"`
}

type ExperimentCondition struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    any    `json:"value"`
	Context  any    `json:"context,omitempty"`
}

type ExperimentPlacementOffering struct {
	PlacementIdentifier string              `json:"placement_identifier"`
	OfferingA           *ExperimentOffering `json:"offering_a,omitempty"`
	OfferingB           *ExperimentOffering `json:"offering_b,omitempty"`
	OfferingC           *ExperimentOffering `json:"offering_c,omitempty"`
	OfferingD           *ExperimentOffering `json:"offering_d,omitempty"`
}

type ExperimentPlacements struct {
	FallbackOfferingA  *ExperimentOffering           `json:"fallback_offering_a,omitempty"`
	FallbackOfferingB  *ExperimentOffering           `json:"fallback_offering_b,omitempty"`
	FallbackOfferingC  *ExperimentOffering           `json:"fallback_offering_c,omitempty"`
	FallbackOfferingD  *ExperimentOffering           `json:"fallback_offering_d,omitempty"`
	PlacementOfferings []ExperimentPlacementOffering `json:"placement_offerings,omitempty"`
}

type ExperimentDurationSettings struct {
	ConversionRatePercentage          *float64 `json:"conversion_rate_percentage,omitempty"`
	DailyEnrolledCustomers            *int     `json:"daily_enrolled_customers,omitempty"`
	MinimumDetectableEffectPercentage *int     `json:"minimum_detectable_effect_percentage,omitempty"`
	ChanceToWinPercentage             *int     `json:"chance_to_win_percentage,omitempty"`
	ConversionRateIsOverride          *bool    `json:"conversion_rate_percentage_is_override,omitempty"`
	DailyEnrolledIsOverride           *bool    `json:"daily_enrolled_customers_is_override,omitempty"`
}

type ExperimentConflict struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name,omitempty"`
}

type Experiment struct {
	Object              string                      `json:"object"`
	ID                  string                      `json:"id"`
	DisplayName         string                      `json:"display_name"`
	Status              string                      `json:"status"`
	CreatedAt           Millis                      `json:"created_at"`
	UpdatedAt           Millis                      `json:"updated_at"`
	StartedAt           *Millis                     `json:"started_at,omitempty"`
	StoppedAt           *Millis                     `json:"stopped_at,omitempty"`
	PausedAt            *Millis                     `json:"paused_at,omitempty"`
	ResumedAt           *Millis                     `json:"resumed_at,omitempty"`
	TotalRunningSeconds *int                        `json:"total_running_time_seconds,omitempty"`
	EnrollmentPercent   *int                        `json:"enrollment_percentage,omitempty"`
	Priority            *int                        `json:"priority,omitempty"`
	OfferingA           *ExperimentOffering         `json:"offering_a,omitempty"`
	OfferingB           *ExperimentOffering         `json:"offering_b,omitempty"`
	OfferingC           *ExperimentOffering         `json:"offering_c,omitempty"`
	OfferingD           *ExperimentOffering         `json:"offering_d,omitempty"`
	Notes               *string                     `json:"notes,omitempty"`
	ExperimentType      *string                     `json:"experiment_type,omitempty"`
	PrimaryMetric       *string                     `json:"primary_metric,omitempty"`
	SecondaryMetrics    []string                    `json:"secondary_metrics,omitempty"`
	EnrollmentMode      *string                     `json:"enrollment_mode,omitempty"`
	AudienceID          *string                     `json:"audience_id,omitempty"`
	TargetingConditions []ExperimentCondition       `json:"targeting_conditions,omitempty"`
	PinnedAudience      any                         `json:"pinned_audience,omitempty"`
	DurationSettings    *ExperimentDurationSettings `json:"experiment_duration_settings,omitempty"`
	Placements          *ExperimentPlacements       `json:"placements,omitempty"`
	Conflicts           []ExperimentConflict        `json:"conflicts,omitempty"`
}

type ListExperimentsOptions struct {
	Status        string
	Limit         int
	StartingAfter string
}

func (s *ExperimentsService) List(ctx context.Context, projectID string, opts ListExperimentsOptions) (*Page[Experiment], error) {
	q := url.Values{}
	if opts.Status != "" {
		q.Set("status", opts.Status)
	}
	if opts.Limit > 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.StartingAfter != "" {
		q.Set("starting_after", opts.StartingAfter)
	}
	path := pathExperiments(projectID)
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var out Page[Experiment]
	err := s.c.do(ctx, http.MethodGet, path, nil, &out)
	return &out, err
}

func (s *ExperimentsService) Get(ctx context.Context, projectID, id string) (*Experiment, error) {
	var out Experiment
	err := s.c.do(ctx, http.MethodGet, pathExperiment(projectID, id), nil, &out)
	return &out, err
}

type ExperimentResults struct {
	Object       string                      `json:"object"`
	Sections     []ExperimentResultsSection  `json:"sections"`
	Currency     string                      `json:"currency"`
	Statistics   []ExperimentResultStatistic `json:"statistics"`
	PredictedLTV *ExperimentPredictedLTV     `json:"predicted_ltv"`
}

type ExperimentResultsSection struct {
	Object      string                    `json:"object"`
	Section     string                    `json:"section"`
	Description *string                   `json:"description,omitempty"`
	Metrics     []ExperimentResultMetric  `json:"metrics"`
	Segments    []ExperimentResultSegment `json:"segments"`
	Values      []ExperimentResultValue   `json:"values"`
}

type ExperimentResultMetric struct {
	Name   string `json:"name"`
	Unit   string `json:"unit,omitempty"`
	Effect string `json:"effect,omitempty"`
}

type ExperimentResultSegment struct {
	Object      string `json:"object"`
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	IsTotal     bool   `json:"is_total"`
}

type ExperimentResultValue struct {
	Object               string   `json:"object"`
	Metric               int      `json:"metric"`
	Segment              int      `json:"segment"`
	Variant              string   `json:"variant"`
	Value                *float64 `json:"value"`
	Change               *float64 `json:"change"`
	CredibleInterval     any      `json:"credible_interval"`
	LiftCredibleInterval any      `json:"lift_credible_interval"`
}

type ExperimentResultStatistic struct {
	Object     string `json:"object"`
	MetricName string `json:"metric_name"`
	Variants   []any  `json:"variants"`
}

type ExperimentPredictedLTV struct {
	Object                 string `json:"object"`
	PredictedWinnerVariant string `json:"predicted_winner_variant"`
	Confidence             int    `json:"confidence"`
}

type ExperimentResultsOptions struct {
	Platform       string
	Country        string
	ExposureStatus string
	Currency       string
}

func (s *ExperimentsService) Results(ctx context.Context, projectID, id string, opts ExperimentResultsOptions) (*ExperimentResults, error) {
	q := url.Values{}
	if opts.Platform != "" {
		q.Set("platform", opts.Platform)
	}
	if opts.Country != "" {
		q.Set("country", opts.Country)
	}
	if opts.ExposureStatus != "" {
		q.Set("exposure_status", opts.ExposureStatus)
	}
	if opts.Currency != "" {
		q.Set("currency", opts.Currency)
	}
	path := pathExperimentResults(projectID, id)
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var out ExperimentResults
	err := s.c.do(ctx, http.MethodGet, path, nil, &out)
	return &out, err
}
