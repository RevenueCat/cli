package api_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

func TestMediaAssetsList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/projects/proj/media_assets" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if q := r.URL.Query(); q.Get("limit") != "5" || q.Get("starting_after") != "medas_prev" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","items":[{"id":"medas_abc","object_name":"media/proj/hero.png","original_name":"hero.png","original_width":1024,"original_height":768,"asset_base_url":"https://assets.example.com","asset_type":"image"}],"next_page":"/v2/projects/proj/media_assets?starting_after=medas_abc","url":"/v2/projects/proj/media_assets"}`))
	}))
	t.Cleanup(srv.Close)

	client := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	page, err := client.MediaAssets.List(context.Background(), "proj", &api.ListOptions{Limit: 5, StartingAfter: "medas_prev"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(page.Items))
	}
	a := page.Items[0]
	if a.ID != "medas_abc" || a.OriginalName != "hero.png" || a.OriginalWidth == nil || *a.OriginalWidth != 1024 {
		t.Fatalf("unexpected asset: %+v", a)
	}
	if page.NextPage == "" {
		t.Fatal("next_page not decoded")
	}
}

func TestMediaAssetsCreate(t *testing.T) {
	raw := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a}
	body := api.CreateMediaAssetJSONBody{
		Filename:       "logo.png",
		ContentType:    api.ImagePng,
		FileDataBase64: base64.StdEncoding.EncodeToString(raw),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/projects/proj/media_assets" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var got api.CreateMediaAssetJSONBody
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got != body {
			t.Fatalf("body = %+v, want %+v", got, body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"medas_abc","formats":{"webp":{"object":"media_asset_format","object_name":"media/proj/logo.webp","size":512,"width":32,"height":null}}}`))
	}))
	t.Cleanup(srv.Close)

	client := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	asset, err := client.MediaAssets.Create(context.Background(), "proj", body)
	if err != nil {
		t.Fatal(err)
	}
	if asset.ID != "medas_abc" {
		t.Fatalf("id = %q, want medas_abc", asset.ID)
	}
	if asset.Formats == nil {
		t.Fatal("formats = nil")
	}
	f, ok := (*asset.Formats)["webp"]
	if !ok || f.ObjectName != "media/proj/logo.webp" || f.Size != 512 || f.Object != "media_asset_format" {
		t.Fatalf("unexpected formats: %+v", asset.Formats)
	}
	if f.Width == nil || *f.Width != 32 || f.Height != nil {
		t.Fatalf("format dimensions = %v/%v, want 32/nil", f.Width, f.Height)
	}
}
