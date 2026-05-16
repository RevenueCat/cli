package api

import (
	"context"
	"net/http"
)

// WebhooksService wraps /integrations/webhooks — the bare /integrations URL
// 404s, the sub-type is part of the path.
type WebhooksService struct{ c *Client }

type Webhook struct {
	ID         string         `json:"id"`
	URL        string         `json:"url,omitempty"`
	Status     string         `json:"status,omitempty"`
	EventTypes []string       `json:"event_types,omitempty"`
	Auth       map[string]any `json:"authorization,omitempty"`
	CreatedAt  Millis         `json:"created_at,omitempty"`
	Object     string         `json:"object,omitempty"`
}

type WebhookCreate struct {
	URL        string         `json:"url"`
	EventTypes []string       `json:"event_types,omitempty"`
	Auth       map[string]any `json:"authorization,omitempty"`
}

type WebhookUpdate struct {
	URL        *string        `json:"url,omitempty"`
	Status     *string        `json:"status,omitempty"`
	EventTypes []string       `json:"event_types,omitempty"`
	Auth       map[string]any `json:"authorization,omitempty"`
}

func (s *WebhooksService) List(ctx context.Context, projectID string) (*Page[Webhook], error) {
	var out Page[Webhook]
	err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "integrations", "webhooks"), nil, &out)
	return &out, err
}

func (s *WebhooksService) Get(ctx context.Context, projectID, id string) (*Webhook, error) {
	var out Webhook
	err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "integrations", "webhooks", id), nil, &out)
	return &out, err
}

func (s *WebhooksService) Create(ctx context.Context, projectID string, body WebhookCreate) (*Webhook, error) {
	var out Webhook
	err := s.c.do(ctx, http.MethodPost, encodePath("projects", projectID, "integrations", "webhooks"), body, &out)
	return &out, err
}

func (s *WebhooksService) Update(ctx context.Context, projectID, id string, body WebhookUpdate) (*Webhook, error) {
	var out Webhook
	err := s.c.do(ctx, http.MethodPost, encodePath("projects", projectID, "integrations", "webhooks", id), body, &out)
	return &out, err
}

func (s *WebhooksService) Delete(ctx context.Context, projectID, id string) error {
	return s.c.do(ctx, http.MethodDelete, encodePath("projects", projectID, "integrations", "webhooks", id), nil, nil)
}
