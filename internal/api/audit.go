package api

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

type AuditService struct{ c *Client }

// AuditLog is loosely typed because the additional_data shape varies per
// action_type. Callers needing strong typing on a specific action should cast
// AdditionalData themselves.
type AuditLog struct {
	ID               string         `json:"id"`
	ActionType       string         `json:"action_type,omitempty"`
	ActorType        string         `json:"actor_type,omitempty"`
	ActorIdentifier  string         `json:"actor_identifier,omitempty"`
	TargetType       string         `json:"target_type,omitempty"`
	TargetIdentifier string         `json:"target_identifier,omitempty"`
	OccurredAt       Millis         `json:"occurred_at,omitempty"`
	ProjectID        string         `json:"project_id,omitempty"`
	AdditionalData   map[string]any `json:"additional_data,omitempty"`
	Object           string         `json:"object,omitempty"`
}

type ListAuditOptions struct {
	Limit         int
	StartingAfter string
}

func (o *ListAuditOptions) query() string {
	if o == nil {
		return ""
	}
	v := url.Values{}
	if o.Limit > 0 {
		v.Set("limit", strconv.Itoa(o.Limit))
	}
	if o.StartingAfter != "" {
		v.Set("starting_after", o.StartingAfter)
	}
	if len(v) == 0 {
		return ""
	}
	return "?" + v.Encode()
}

func (s *AuditService) List(ctx context.Context, projectID string, opts *ListAuditOptions) (*Page[AuditLog], error) {
	var out Page[AuditLog]
	err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "audit_logs")+opts.query(), nil, &out)
	return &out, err
}
