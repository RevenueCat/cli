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

type PaywallCreate struct {
	OfferingID                 string `json:"offering_id"`
	AutomaticallyScaleFontSize bool   `json:"automatically_scale_font_size"`
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
	DefaultLocale           string          `json:"default_locale"`
	Name                    *string         `json:"name,omitempty"`
}

func (s *PaywallsService) List(ctx context.Context, projectID string) (*Page[Paywall], error) {
	var out Page[Paywall]
	err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "paywalls"), nil, &out)
	return &out, err
}

func (s *PaywallsService) Get(ctx context.Context, projectID, id string) (*Paywall, error) {
	var out Paywall
	err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "paywalls", id), nil, &out)
	return &out, err
}

// GetWithComponents fetches a paywall including its published and draft
// component versions.
func (s *PaywallsService) GetWithComponents(ctx context.Context, projectID, id string) (*Paywall, error) {
	var out Paywall
	path := encodePath("projects", projectID, "paywalls", id) + "?expand=components"
	err := s.c.do(ctx, http.MethodGet, path, nil, &out)
	return &out, err
}

// UpdateDraft saves component state onto the paywall draft.
func (s *PaywallsService) UpdateDraft(ctx context.Context, projectID, id string, body PaywallDraftUpdate) (*Paywall, error) {
	var out Paywall
	err := s.c.do(ctx, http.MethodPatch, encodePath("projects", projectID, "paywalls", id), body, &out)
	return &out, err
}

func (s *PaywallsService) Create(ctx context.Context, projectID string, body PaywallCreate) (*Paywall, error) {
	var out Paywall
	err := s.c.do(ctx, http.MethodPost, encodePath("projects", projectID, "paywalls"), body, &out)
	return &out, err
}

func (s *PaywallsService) Publish(ctx context.Context, projectID, id string) (*Paywall, error) {
	var out Paywall
	path := encodePath("projects", projectID, "paywalls", id, "actions") + "/publish"
	err := s.c.do(ctx, http.MethodPost, path, nil, &out)
	return &out, err
}

func (s *PaywallsService) Unpublish(ctx context.Context, projectID, id string) (*Paywall, error) {
	var out Paywall
	path := encodePath("projects", projectID, "paywalls", id, "actions") + "/unpublish"
	err := s.c.do(ctx, http.MethodPost, path, nil, &out)
	return &out, err
}

func (s *PaywallsService) Delete(ctx context.Context, projectID, id string) error {
	return s.c.do(ctx, http.MethodDelete, encodePath("projects", projectID, "paywalls", id), nil, nil)
}
