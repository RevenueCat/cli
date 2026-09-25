package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
)

type TargetingRulesService struct{ c *Client }

type TargetingRule struct {
	Object      string  `json:"object"`
	ID          string  `json:"id"`
	RuleType    string  `json:"rule_type"`
	State       string  `json:"state"`
	DisplayName string  `json:"display_name"`
	OfferingID  string  `json:"offering_id,omitempty"`
	AudienceID  *string `json:"audience_id,omitempty"`
	Conditions  []any   `json:"conditions,omitempty"`
	Schedule    any     `json:"schedule,omitempty"`
	Placements  any     `json:"placements,omitempty"`
	FlowID      string  `json:"flow_id,omitempty"`
	Checkpoints []any   `json:"checkpoints,omitempty"`
}

type ListTargetingRulesOptions struct {
	State         string
	Limit         int
	StartingAfter string
}

func (s *TargetingRulesService) List(ctx context.Context, projectID string, opts ListTargetingRulesOptions) (*Page[TargetingRule], error) {
	q := url.Values{}
	if opts.State != "" {
		q.Set("state", opts.State)
	}
	if opts.Limit > 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.StartingAfter != "" {
		q.Set("starting_after", opts.StartingAfter)
	}
	path := pathTargetingRules(projectID)
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var out Page[TargetingRule]
	err := s.c.do(ctx, http.MethodGet, path, nil, &out)
	return &out, err
}

func (s *TargetingRulesService) Get(ctx context.Context, projectID, id string) (*TargetingRule, error) {
	var out TargetingRule
	err := s.c.do(ctx, http.MethodGet, pathTargetingRule(projectID, id), nil, &out)
	return &out, err
}

type TargetingRuleCreate struct {
	RuleType    string          `json:"rule_type,omitempty"`
	Position    *int            `json:"position,omitempty"`
	ID          string          `json:"id,omitempty"`
	State       string          `json:"state,omitempty"`
	DisplayName string          `json:"display_name"`
	OfferingID  string          `json:"offering_id,omitempty"`
	AudienceID  string          `json:"audience_id,omitempty"`
	Conditions  json.RawMessage `json:"conditions,omitempty"`
	Schedule    json.RawMessage `json:"schedule,omitempty"`
	Placements  json.RawMessage `json:"placements,omitempty"`
	FlowID      string          `json:"flow_id,omitempty"`
	Checkpoints json.RawMessage `json:"checkpoints,omitempty"`
}

type TargetingRuleUpdate map[string]json.RawMessage

func (s *TargetingRulesService) Create(ctx context.Context, projectID string, body TargetingRuleCreate) (*TargetingRule, error) {
	var out TargetingRule
	err := s.c.do(ctx, http.MethodPost, pathTargetingRules(projectID), body, &out)
	return &out, err
}

func (s *TargetingRulesService) Update(ctx context.Context, projectID, id string, body TargetingRuleUpdate) (*TargetingRule, error) {
	var out TargetingRule
	err := s.c.do(ctx, http.MethodPost, pathTargetingRule(projectID, id), body, &out)
	return &out, err
}

func (s *TargetingRulesService) Delete(ctx context.Context, projectID, id string) error {
	return s.c.do(ctx, http.MethodDelete, pathTargetingRule(projectID, id), nil, nil)
}
