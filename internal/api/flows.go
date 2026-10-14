package api

import (
	"context"
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
