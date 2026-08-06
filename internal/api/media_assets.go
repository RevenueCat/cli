package api

import (
	"context"
	"encoding/json"
	"net/http"
)

type MediaAssetsService struct{ c *Client }

type MediaAssetCreate struct {
	Filename       string `json:"filename"`
	ContentType    string `json:"content_type"`
	FileDataBase64 string `json:"file_data_base64"`
}

type MediaAssetFormat struct {
	ObjectName string `json:"object_name"`
	Size       int64  `json:"size"`
	Width      *int   `json:"width"`
	Height     *int   `json:"height"`
	Object     string `json:"object,omitempty"`
}

type MediaAsset struct {
	ID                string                      `json:"id"`
	ObjectName        string                      `json:"object_name"`
	OriginalName      string                      `json:"original_name"`
	OriginalSize      int64                       `json:"original_size"`
	OriginalWidth     *int                        `json:"original_width"`
	OriginalHeight    *int                        `json:"original_height"`
	Formats           map[string]MediaAssetFormat `json:"formats"`
	AltText           *string                     `json:"alt_text"`
	IsDecorative      bool                        `json:"is_decorative"`
	AssetBaseURL      *string                     `json:"asset_base_url"`
	AssetType         string                      `json:"asset_type"`
	VideoMetadata     json.RawMessage             `json:"video_metadata"`
	TranscodingStatus *string                     `json:"transcoding_status"`
	Object            string                      `json:"object,omitempty"`
}

func (s *MediaAssetsService) Create(ctx context.Context, projectID string, body MediaAssetCreate) (*MediaAsset, error) {
	var out MediaAsset
	err := s.c.do(ctx, http.MethodPost, encodePath("projects", projectID, "media_assets"), body, &out)
	return &out, err
}
