package api

import (
	"context"
	"net/http"
	"net/url"
)

type StoreStatePlansService struct{ c *Client }

type StoreStatePlanDesiredState struct {
	ProductID               string                       `json:"product_id,omitempty"`
	CreateRevenueCatProduct *StoreStatePlanProductCreate `json:"create_revenuecat_product,omitempty"`
	Store                   string                       `json:"store"`
	Common                  map[string]any               `json:"common,omitempty"`
	StoreState              map[string]any               `json:"store_state,omitempty"`
}

type StoreStatePlanProductCreate struct {
	AppID           string `json:"app_id"`
	StoreIdentifier string `json:"store_identifier"`
	Type            string `json:"type"`
	DisplayName     string `json:"display_name"`
	Title           string `json:"title"`
}

type StoreStatePlanCreate struct {
	DesiredStates []StoreStatePlanDesiredState `json:"desired_states"`
}

type StoreStatePlanActionResponse struct {
	ID         string `json:"id"`
	Object     string `json:"object"`
	Status     string `json:"status"`
	PollingURL string `json:"polling_url,omitempty"`
}

type StoreStatePlan struct {
	ID            string                       `json:"id"`
	Object        string                       `json:"object"`
	Status        string                       `json:"status"`
	HasChanges    *bool                        `json:"has_changes"`
	Actions       []string                     `json:"actions"`
	Summary       *StoreStatePlanSummary       `json:"summary"`
	DesiredStates []StoreStatePlanDesiredState `json:"desired_states"`
	PlanItems     []StoreStatePlanItem         `json:"plan_items"`
	ErrorMessage  *string                      `json:"error_message"`
	Warnings      []StoreStatePlanWarning      `json:"warnings"`
}

type StoreStatePlanSummary struct {
	ProductsAdded     int64 `json:"products_added"`
	ProductsModified  int64 `json:"products_modified"`
	ProductsUnchanged int64 `json:"products_unchanged"`
}

type StoreStatePlanItem struct {
	ProductID         *string                 `json:"product_id"`
	AppID             *string                 `json:"app_id"`
	StoreIdentifier   *string                 `json:"store_identifier"`
	Action            string                  `json:"action"`
	Diff              []StoreStatePlanDiff    `json:"diff"`
	Warnings          []StoreStatePlanWarning `json:"warnings"`
	ErrorMessage      *string                 `json:"error_message"`
	ApplyStatus       *string                 `json:"apply_status"`
	ApplyErrorMessage *string                 `json:"apply_error_message"`
}

type StoreStatePlanDiff struct {
	Field     string `json:"field"`
	FromValue any    `json:"from_value"`
	ToValue   any    `json:"to_value"`
}

type StoreStatePlanWarning struct {
	Severity string `json:"severity"`
	Field    string `json:"field"`
	Message  string `json:"message"`
}

func (s *StoreStatePlansService) Create(ctx context.Context, projectID string, body StoreStatePlanCreate) (*StoreStatePlanActionResponse, error) {
	var out StoreStatePlanActionResponse
	err := s.c.do(ctx, http.MethodPost, pathStoreStatePlans(projectID), body, &out)
	return &out, err
}

func (s *StoreStatePlansService) List(ctx context.Context, projectID, status string) (*Page[StoreStatePlan], error) {
	var out Page[StoreStatePlan]
	path := pathStoreStatePlans(projectID)
	if status != "" {
		path += "?status=" + url.QueryEscape(status)
	}
	err := s.c.do(ctx, http.MethodGet, path, nil, &out)
	return &out, err
}

func (s *StoreStatePlansService) Get(ctx context.Context, projectID, planID string) (*StoreStatePlan, error) {
	var out StoreStatePlan
	err := s.c.do(ctx, http.MethodGet, pathStoreStatePlan(projectID, planID), nil, &out)
	return &out, err
}

func (s *StoreStatePlansService) Plan(ctx context.Context, projectID, planID string) (*StoreStatePlanActionResponse, error) {
	return s.action(ctx, projectID, planID, "plan")
}

func (s *StoreStatePlansService) Apply(ctx context.Context, projectID, planID string) (*StoreStatePlanActionResponse, error) {
	return s.action(ctx, projectID, planID, "apply")
}

func (s *StoreStatePlansService) Discard(ctx context.Context, projectID, planID string) (*StoreStatePlanActionResponse, error) {
	return s.action(ctx, projectID, planID, "discard")
}

func (s *StoreStatePlansService) action(ctx context.Context, projectID, planID, action string) (*StoreStatePlanActionResponse, error) {
	var out StoreStatePlanActionResponse
	// Inline path: the spec models each action as a fixed path; this dispatches a
	// dynamic action, so there's no single generated builder to use.
	path := encodePath("projects", projectID, "store_state", "plans", planID, "actions", action)
	err := s.c.do(ctx, http.MethodPost, path, struct{}{}, &out)
	return &out, err
}
