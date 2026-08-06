package api_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

func TestMediaAssetsCreate(t *testing.T) {
	raw := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/projects/proj/media_assets" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body api.MediaAssetCreate
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Filename != "logo.png" || body.ContentType != "image/png" {
			t.Fatalf("unexpected body: %+v", body)
		}
		decoded, err := base64.StdEncoding.DecodeString(body.FileDataBase64)
		if err != nil {
			t.Fatalf("file_data_base64 is not valid base64: %v", err)
		}
		if !bytes.Equal(decoded, raw) {
			t.Fatalf("file_data_base64 decodes to %v, want %v", decoded, raw)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{
			"object": "media_asset",
			"id": "medas_abc",
			"object_name": "media/proj/logo.png",
			"original_name": "logo.png",
			"original_size": 1,
			"original_width": 32,
			"original_height": 16,
			"formats": {"webp": {"object_name": "media/proj/logo.webp", "size": 512, "width": 32, "height": 16}},
			"alt_text": null,
			"is_decorative": false,
			"asset_base_url": "https://assets.example.com",
			"asset_type": "image",
			"video_metadata": null,
			"transcoding_status": null
		}`))
	}))
	t.Cleanup(srv.Close)

	client := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	asset, err := client.MediaAssets.Create(context.Background(), "proj", api.MediaAssetCreate{
		Filename:       "logo.png",
		ContentType:    "image/png",
		FileDataBase64: base64.StdEncoding.EncodeToString(raw),
	})
	if err != nil {
		t.Fatal(err)
	}
	if asset.ID != "medas_abc" || asset.ObjectName != "media/proj/logo.png" || asset.AssetType != "image" {
		t.Fatalf("unexpected asset: %+v", asset)
	}
	if asset.OriginalWidth == nil || *asset.OriginalWidth != 32 {
		t.Fatalf("original_width = %v, want 32", asset.OriginalWidth)
	}
	if asset.AltText != nil || asset.TranscodingStatus != nil {
		t.Fatalf("nullable fields should be nil: %+v", asset)
	}
	f, ok := asset.Formats["webp"]
	if !ok || f.ObjectName != "media/proj/logo.webp" || f.Size != 512 {
		t.Fatalf("unexpected formats: %+v", asset.Formats)
	}
}
