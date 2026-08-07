package api

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

type FontsService struct{ c *Client }

type ListFontsOptions struct {
	Limit         int
	StartingAfter string
}

func (o *ListFontsOptions) query() string {
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

type FontCreate struct {
	Filename       string `json:"filename"`
	ContentType    string `json:"content_type"`
	FileDataBase64 string `json:"file_data_base64"`
}

type Font struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	FamilyName string `json:"family_name"`
	Style      string `json:"style"`
	Weight     int    `json:"weight"`
	URL        string `json:"url"`
	FontKey    string `json:"font_key"`
	Object     string `json:"object,omitempty"`
}

func (s *FontsService) List(ctx context.Context, projectID string, opts *ListFontsOptions) (*Page[Font], error) {
	var out Page[Font]
	if err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "fonts")+opts.query(), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *FontsService) Create(ctx context.Context, projectID string, body FontCreate) (*Font, error) {
	var out Font
	err := s.c.do(ctx, http.MethodPost, encodePath("projects", projectID, "fonts"), body, &out)
	return &out, err
}
