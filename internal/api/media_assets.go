package api

import (
	"context"
	"net/http"
)

type MediaAssetsService struct{ c *Client }

// MediaAsset, MediaAssetFormat, and CreateMediaAssetJSONBody are generated in types_gen.go.

func (s *MediaAssetsService) Create(ctx context.Context, projectID string, body CreateMediaAssetJSONBody) (*MediaAsset, error) {
	var out MediaAsset
	err := s.c.do(ctx, http.MethodPost, encodePath("projects", projectID, "media_assets"), body, &out)
	return &out, err
}
