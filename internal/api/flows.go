package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
)

// FlowsService wraps /flows. Feedback surveys are not returned by the API.
type FlowsService struct{ c *Client }

type Flow struct {
	ID                    string  `json:"id"`
	Object                string  `json:"object,omitempty"`
	Name                  string  `json:"name"`
	ProjectID             string  `json:"project_id,omitempty"`
	Type                  string  `json:"type"`
	CreatedAt             Millis  `json:"created_at,omitempty"`
	PublishedAt           *Millis `json:"published_at"`
	HasUnpublishedChanges bool    `json:"has_unpublished_changes"`
	Revision              int     `json:"revision"`
}

type FlowSettings struct {
	DisplayName                   *string         `json:"display_name,omitempty"`
	InitialStepID                 json.RawMessage `json:"initial_step_id,omitempty"`
	SingleStepFallbackID          json.RawMessage `json:"single_step_fallback_id,omitempty"`
	AppID                         json.RawMessage `json:"app_id,omitempty"`
	Metadata                      json.RawMessage `json:"metadata,omitempty"`
	CustomContextParametersConfig json.RawMessage `json:"custom_context_parameters_config,omitempty"`
	ContentMaxWidth               json.RawMessage `json:"content_max_width,omitempty"`
}

type FlowDraft struct {
	FlowSettings
	ID       string                     `json:"id"`
	Object   string                     `json:"object"`
	Type     string                     `json:"type"`
	Revision int                        `json:"revision"`
	Steps    map[string]json.RawMessage `json:"steps"`
}

type UpdateFlowDraft struct {
	FlowSettings
	Revision       int                        `json:"revision"`
	UpdatedSteps   map[string]json.RawMessage `json:"updated_steps,omitempty"`
	DeletedStepIDs []string                   `json:"deleted_step_ids,omitempty"`
}

type CreateFlow struct {
	FlowSettings
	Type  string                     `json:"type,omitempty"`
	Steps map[string]json.RawMessage `json:"steps,omitempty"`
}

type FlowValidationIssue struct {
	Key            string  `json:"key"`
	Message        string  `json:"message"`
	Severity       string  `json:"severity"`
	ComponentID    *string `json:"component_id,omitempty"`
	Property       *string `json:"property,omitempty"`
	Locale         *string `json:"locale,omitempty"`
	LocalizationID *string `json:"localization_id,omitempty"`
	Path           *string `json:"path,omitempty"`
	Action         *string `json:"action,omitempty"`
}

// FlowListOptions filters by Flow type. Type and ExcludeType can't be combined.
type FlowListOptions struct {
	ListOptions
	Type        []string
	ExcludeType []string
}

func (o FlowListOptions) query() string {
	v := url.Values{}
	if o.Limit > 0 {
		v.Set("limit", strconv.Itoa(o.Limit))
	}
	if o.StartingAfter != "" {
		v.Set("starting_after", o.StartingAfter)
	}
	for _, t := range o.Type {
		v.Add("type", t)
	}
	for _, t := range o.ExcludeType {
		v.Add("exclude_type", t)
	}
	if len(v) == 0 {
		return ""
	}
	return "?" + v.Encode()
}

func (s *FlowsService) List(ctx context.Context, projectID string, opts FlowListOptions) (*Page[Flow], error) {
	var out Page[Flow]
	if err := s.c.do(ctx, http.MethodGet, pathFlows(projectID)+opts.query(), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *FlowsService) GetDraft(ctx context.Context, projectID, id string) (*FlowDraft, error) {
	var out FlowDraft
	err := s.c.do(ctx, http.MethodGet, pathFlowDraft(projectID, id), nil, &out)
	return &out, err
}

func (s *FlowsService) UpdateDraft(ctx context.Context, projectID, id string, body UpdateFlowDraft) (*Flow, error) {
	var out Flow
	err := s.c.do(ctx, http.MethodPatch, pathFlowDraft(projectID, id), body, &out)
	return &out, err
}

func (s *FlowsService) Create(ctx context.Context, projectID string, body CreateFlow) (*Flow, error) {
	var out Flow
	err := s.c.do(ctx, http.MethodPost, pathFlows(projectID), body, &out)
	return &out, err
}

func (s *FlowsService) Duplicate(ctx context.Context, projectID, id string, body DuplicateFlow) (*Flow, error) {
	var out Flow
	err := s.c.do(ctx, http.MethodPost, pathFlowActionsDuplicate(projectID, id), body, &out)
	return &out, err
}

func (s *FlowsService) Validate(ctx context.Context, projectID, id string, body ValidateFlow) (*FlowValidation, error) {
	var out FlowValidation
	err := s.c.do(ctx, http.MethodPost, pathFlowActionsValidate(projectID, id), body, &out)
	return &out, err
}
