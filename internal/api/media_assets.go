package api

import (
	"context"
	"net/http"
)

type MediaAssetsService struct{ c *Client }

func (s *MediaAssetsService) List(ctx context.Context, projectID string, opts *ListOptions) (*Page[MediaAsset], error) {
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
