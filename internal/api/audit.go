package api

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

type AuditService struct{ c *Client }

// AuditLog type is generated in types_gen.go.

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
	err := s.c.do(ctx, http.MethodGet, pathAuditLogs(projectID)+opts.query(), nil, &out)
	return &out, err
}
