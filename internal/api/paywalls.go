package api

import (
	"context"
	"encoding/json"
	"net/http"
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
	AutomaticallyScaleFontSize *bool           `json:"automatically_scale_font_size"`
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
	StateDeclarations          json.RawMessage `json:"state_declarations,omitempty"`
	DefaultLocale              string          `json:"default_locale"`
	Name                       *string         `json:"name,omitempty"`
	AutomaticallyScaleFontSize *bool           `json:"automatically_scale_font_size,omitempty"`
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

type PaywallValidationRequest struct {
	Paywall PaywallValidationPaywall `json:"paywall"`
	Context PaywallValidationContext `json:"context"`
}

type PaywallValidationPaywall struct {
	RCPublicID              string          `json:"rc_public_id"`
	Name                    *string         `json:"name"`
	TemplateName            string          `json:"template_name"`
	OfferingID              *string         `json:"offering_id"`
	DefaultLocale           string          `json:"default_locale"`
	ComponentsConfig        json.RawMessage `json:"components_config"`
	ComponentsLocalizations json.RawMessage `json:"components_localizations"`
	StateDeclarations       json.RawMessage `json:"state_declarations"`
}

type PaywallValidationContext struct {
	Offering            *PaywallValidationOffering `json:"offering"`
	UIConfig            json.RawMessage            `json:"ui_config"`
	IsStandaloneBuilder bool                       `json:"is_standalone_builder"`
}

type PaywallValidationOffering struct {
	ID          string                     `json:"id"`
	Identifier  string                     `json:"identifier"`
	DisplayName string                     `json:"display_name"`
	IsCurrent   bool                       `json:"is_current"`
	Packages    []PaywallValidationPackage `json:"packages"`
}

type PaywallValidationPackage struct {
	ID          string                     `json:"id"`
	Identifier  string                     `json:"identifier"`
	DisplayName string                     `json:"display_name"`
	Products    []PaywallValidationProduct `json:"products"`
}

type PaywallValidationProduct struct {
	ID  string                   `json:"id"`
	App *PaywallValidationAppRef `json:"app"`
}

type PaywallValidationAppRef struct {
	Type string `json:"type"`
}

type PaywallValidationResult struct {
	Valid  bool                     `json:"valid"`
	Issues []PaywallValidationIssue `json:"issues"`
}

type PaywallValidationIssue struct {
	Key         string `json:"key"`
	Severity    string `json:"severity"`
	Message     string `json:"message"`
	ComponentID string `json:"component_id,omitempty"`
	Property    string `json:"property,omitempty"`
	Locale      string `json:"locale,omitempty"`
	Path        string `json:"path,omitempty"`
}

func (s *PaywallsService) Validate(ctx context.Context, projectID string, body PaywallValidationRequest) (*PaywallValidationResult, error) {
	var out PaywallValidationResult
	err := s.c.do(ctx, http.MethodPost, pathPaywallsActionsValidate(projectID), body, &out)
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
