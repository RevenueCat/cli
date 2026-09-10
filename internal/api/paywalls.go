package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

type PaywallsService struct{ c *Client }

type Paywall struct {
	ID                         string             `json:"id"`
	Name                       string             `json:"name,omitempty"`
	OfferingID                 string             `json:"offering_id,omitempty"`
	AutomaticallyScaleFontSize bool               `json:"automatically_scale_font_size,omitempty"`
	CreatedAt                  Millis             `json:"created_at,omitempty"`
	PublishedAt                *Millis            `json:"published_at"`
	Components                 *PaywallComponents `json:"components,omitempty"`
	Object                     string             `json:"object,omitempty"`
}

// PaywallComponents carries the published and draft component versions when
// a paywall is fetched with expand=components.
type PaywallComponents struct {
	Published *PaywallComponentsVersion `json:"published"`
	Draft     *PaywallComponentsVersion `json:"draft"`
}

type PaywallComponentsVersion struct {
	Revision                   *int            `json:"revision"`
	ComponentsConfig           json.RawMessage `json:"components_config"`
	ComponentsLocalizations    json.RawMessage `json:"components_localizations"`
	StateDeclarations          json.RawMessage `json:"state_declarations,omitempty"`
	DefaultLocale              string          `json:"default_locale"`
	AutomaticallyScaleFontSize bool            `json:"automatically_scale_font_size"`
}

// PaywallDraftUpdate is the PATCH body that saves component state onto a
// paywall draft. Revision must match the server's current draft revision;
// stale writes are rejected with HTTP 409.
type PaywallDraftUpdate struct {
	Revision                int             `json:"revision"`
	ComponentsConfig        json.RawMessage `json:"components_config"`
	ComponentsLocalizations json.RawMessage `json:"components_localizations"`
	// Unset must marshal as an omitted field, never as null: the server keeps
	// stored declarations when the field is absent but clears them on explicit null.
	StateDeclarations json.RawMessage `json:"state_declarations,omitempty"`
	DefaultLocale     string          `json:"default_locale"`
	Name              *string         `json:"name,omitempty"`
}

func (s *PaywallsService) List(ctx context.Context, projectID string) (*Page[Paywall], error) {
	var out Page[Paywall]
	err := s.c.do(ctx, http.MethodGet, pathPaywalls(projectID), nil, &out)
	return &out, err
}

func (s *PaywallsService) Get(ctx context.Context, projectID, id string) (*Paywall, error) {
	var out Paywall
	err := s.c.do(ctx, http.MethodGet, pathPaywall(projectID, id), nil, &out)
	return &out, err
}

// GetWithComponents fetches a paywall including its published and draft
// component versions.
func (s *PaywallsService) GetWithComponents(ctx context.Context, projectID, id string) (*Paywall, error) {
	var out Paywall
	path := pathPaywall(projectID, id) + "?expand=components"
	err := s.c.do(ctx, http.MethodGet, path, nil, &out)
	return &out, err
}

// UpdateDraft saves component state onto the paywall draft.
func (s *PaywallsService) UpdateDraft(ctx context.Context, projectID, id string, body PaywallDraftUpdate) (*Paywall, error) {
	var out Paywall
	err := s.c.do(ctx, http.MethodPatch, pathPaywall(projectID, id), body, &out)
	return &out, err
}

// PaywallComponentsCreate is the from-components create variant: offering
// optional, so paywalls can exist standalone and attach later.
type PaywallComponentsCreate struct {
	OfferingID              string          `json:"offering_id,omitempty"`
	Name                    string          `json:"name,omitempty"`
	ComponentsConfig        json.RawMessage `json:"components_config"`
	ComponentsLocalizations json.RawMessage `json:"components_localizations"`
	DefaultLocale           string          `json:"default_locale,omitempty"`
}

func (s *PaywallsService) CreateFromComponents(ctx context.Context, projectID string, body PaywallComponentsCreate) (*Paywall, error) {
	var out Paywall
	err := s.c.do(ctx, http.MethodPost, pathPaywalls(projectID), body, &out)
	return &out, err
}

// SetOffering attaches (non-nil) or detaches (nil) the paywall's offering.
func (s *PaywallsService) SetOffering(ctx context.Context, projectID, id string, revision int, offeringID *string) (*Paywall, error) {
	body := map[string]any{"revision": revision, "offering_id": offeringID}
	var out Paywall
	err := s.c.do(ctx, http.MethodPatch, pathPaywall(projectID, id), body, &out)
	return &out, err
}

func (s *PaywallsService) Publish(ctx context.Context, projectID, id string) (*Paywall, error) {
	var out Paywall
	path := pathPaywallActionsPublish(projectID, id)
	err := s.c.do(ctx, http.MethodPost, path, nil, &out)
	return &out, err
}

func (s *PaywallsService) Unpublish(ctx context.Context, projectID, id string) (*Paywall, error) {
	var out Paywall
	path := pathPaywallActionsUnpublish(projectID, id)
	err := s.c.do(ctx, http.MethodPost, path, nil, &out)
	return &out, err
}

func (s *PaywallsService) Delete(ctx context.Context, projectID, id string) error {
	return s.c.do(ctx, http.MethodDelete, pathPaywall(projectID, id), nil, nil)
}

// PaywallGraph is the envelope from GET .../paywalls/{id}/graph. Graph is nil
// only for a genuinely standalone V2 paywall with no screen graph.
//
// Hand-written, not generated: this route isn't in the vendored OpenAPI spec
// yet, so it's listed in scripts/gen-paths.py's NON_SPEC_PATHS instead of
// getting a generated type.
type PaywallGraph struct {
	Object  string `json:"object"`
	ID      string `json:"id"`
	Version string `json:"version"`
	Graph   *Graph `json:"graph"`
}

// Graph describes a paywall's screens and how they connect. PaywallStepID is
// the authoritative purchase screen; a step's IsTerminal means "no outgoing
// edges", not "is the purchase screen" — do not conflate the two.
type Graph struct {
	Revision      *int        `json:"revision"`
	InitialStepID *string     `json:"initial_step_id"`
	PaywallStepID string      `json:"paywall_step_id"`
	TotalSteps    int         `json:"total_steps"`
	Steps         []GraphStep `json:"steps"`
}

// GraphStep is one node in the graph. Paywall carries the step's editable
// content and is present only when the graph is fetched with
// expand=graph.steps.paywall.
type GraphStep struct {
	ID              string           `json:"id"`
	Name            *string          `json:"name"`
	Type            string           `json:"type"`
	ScreenTypes     []string         `json:"screen_types"`
	IsTerminal      bool             `json:"is_terminal"`
	PaywallID       *string          `json:"paywall_id"`
	Edges           []GraphEdge      `json:"edges"`
	UnwiredTriggers []UnwiredTrigger `json:"unwired_triggers"`
	Paywall         *ScreenContent   `json:"paywall,omitempty"`
}

type GraphEdge struct {
	To                 string          `json:"to"`
	TriggerID          string          `json:"trigger_id"`
	Condition          json.RawMessage `json:"condition"`
	IsDefault          bool            `json:"is_default"`
	TriggerComponentID *string         `json:"trigger_component_id"`
	TriggerName        *string         `json:"trigger_name"`
	TriggerType        *string         `json:"trigger_type"`
}

type UnwiredTrigger struct {
	ActionID    string  `json:"action_id"`
	ComponentID *string `json:"component_id"`
	Name        *string `json:"name"`
	Type        *string `json:"type"`
	Reason      string  `json:"reason"`
}

// ScreenContent is a graph step's editable content — the same fields the
// dashboard's paywall editor reads and writes, scoped to one screen.
type ScreenContent struct {
	ID                      string          `json:"id"`
	Revision                int             `json:"revision"`
	ComponentsConfig        json.RawMessage `json:"components_config"`
	ComponentsLocalizations json.RawMessage `json:"components_localizations"`
	DefaultLocale           string          `json:"default_locale"`
	StateDeclarations       json.RawMessage `json:"state_declarations"`
}

// GetGraph fetches a paywall's screen topology (no screen content). version
// is "draft" or "published".
func (s *PaywallsService) GetGraph(ctx context.Context, projectID, id, version string) (*PaywallGraph, error) {
	path := encodePath("projects", projectID, "paywalls", id, "graph") + "?version=" + url.QueryEscape(version)
	var out PaywallGraph
	err := s.c.do(ctx, http.MethodGet, path, nil, &out)
	return &out, err
}

// GetGraphWithScreenContent fetches the graph with each step's editable
// content included — the only way to read a sibling screen's content; the
// plain paywall GET can only ever return the fallback screen's own version.
func (s *PaywallsService) GetGraphWithScreenContent(ctx context.Context, projectID, id, version string) (*PaywallGraph, error) {
	path := encodePath("projects", projectID, "paywalls", id, "graph") +
		"?version=" + url.QueryEscape(version) + "&expand=graph.steps.paywall"
	var out PaywallGraph
	err := s.c.do(ctx, http.MethodGet, path, nil, &out)
	return &out, err
}

// UpdateDraftStep saves component state onto one screen selected from the
// paywall's graph instead of onto its fallback draft. parentID stays in the
// path; the target screen is addressed only by the step_id query param, and
// the response's id is that screen's own canonical id, which can differ from
// parentID. The API rejects name for a sibling screen, so this clears Name
// regardless of what the caller set.
func (s *PaywallsService) UpdateDraftStep(ctx context.Context, projectID, parentID, stepID string, body PaywallDraftUpdate) (*Paywall, error) {
	body.Name = nil
	path := pathPaywall(projectID, parentID) + "?step_id=" + url.QueryEscape(stepID)
	var out Paywall
	err := s.c.do(ctx, http.MethodPatch, path, body, &out)
	return &out, err
}
