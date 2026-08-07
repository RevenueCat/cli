package api

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

type MediaAssetsService struct{ c *Client }

type ListMediaAssetsOptions struct {
	Limit         int
	StartingAfter string
}

func (o *ListMediaAssetsOptions) query() string {
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

func (s *MediaAssetsService) List(ctx context.Context, projectID string, opts *ListMediaAssetsOptions) (*Page[MediaAsset], error) {
	var out Page[MediaAsset]
	if err := s.c.do(ctx, http.MethodGet, pathMediaAssets(projectID)+opts.query(), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *MediaAssetsService) Create(ctx context.Context, projectID string, body CreateMediaAssetJSONBody) (*MediaAsset, error) {
	var out MediaAsset
	err := s.c.do(ctx, http.MethodPost, pathMediaAssets(projectID), body, &out)
	return &out, err
}
