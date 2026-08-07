package api

import (
	"context"
	"net/http"
)

// WebhooksService wraps /integrations/webhooks — the bare /integrations URL
// 404s, the sub-type is part of the path.
type WebhooksService struct{ c *Client }

// Fields mirror the v2 WebhookIntegration schema: it carries name and
// environment (there is no "status"/pause concept), and the auth header is a
// single string field, authorization_header — not a map.
type Webhook struct {
	ID          string   `json:"id"`
	Name        string   `json:"name,omitempty"`
	URL         string   `json:"url,omitempty"`
	Environment string   `json:"environment,omitempty"`
	EventTypes  []string `json:"event_types,omitempty"`
	AppID       string   `json:"app_id,omitempty"`
	CreatedAt   Millis   `json:"created_at,omitempty"`
	Object      string   `json:"object,omitempty"`
}

type WebhookCreate struct {
	Name                string   `json:"name"` // required by the API
	URL                 string   `json:"url"`
	EventTypes          []string `json:"event_types,omitempty"`
	AuthorizationHeader string   `json:"authorization_header,omitempty"`
}

type WebhookUpdate struct {
	Name                *string  `json:"name,omitempty"`
	URL                 *string  `json:"url,omitempty"`
	EventTypes          []string `json:"event_types,omitempty"`
	AuthorizationHeader *string  `json:"authorization_header,omitempty"`
}

func (s *WebhooksService) List(ctx context.Context, projectID string) (*Page[Webhook], error) {
	var out Page[Webhook]
	err := s.c.do(ctx, http.MethodGet, pathIntegrationsWebhooks(projectID), nil, &out)
	return &out, err
}

func (s *WebhooksService) Get(ctx context.Context, projectID, id string) (*Webhook, error) {
	var out Webhook
	err := s.c.do(ctx, http.MethodGet, pathIntegrationsWebhook(projectID, id), nil, &out)
	return &out, err
}

func (s *WebhooksService) Create(ctx context.Context, projectID string, body WebhookCreate) (*Webhook, error) {
	var out Webhook
	err := s.c.do(ctx, http.MethodPost, pathIntegrationsWebhooks(projectID), body, &out)
	return &out, err
}

func (s *WebhooksService) Update(ctx context.Context, projectID, id string, body WebhookUpdate) (*Webhook, error) {
	var out Webhook
	err := s.c.do(ctx, http.MethodPost, pathIntegrationsWebhook(projectID, id), body, &out)
	return &out, err
}

func (s *WebhooksService) Delete(ctx context.Context, projectID, id string) error {
	return s.c.do(ctx, http.MethodDelete, pathIntegrationsWebhook(projectID, id), nil, nil)
}
