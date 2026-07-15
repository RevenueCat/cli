package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

func TestProjectsCreate_PostsBodyShape(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/projects" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var err error
		body, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"proj_new","name":"New Project","object":"project","created_at":1}`))
	}))
	t.Cleanup(srv.Close)

	c := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	if _, err := c.Projects.Create(context.Background(), api.ProjectCreate{Name: "New Project"}); err != nil {
		t.Fatal(err)
	}
	var got api.ProjectCreate
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "New Project" {
		t.Fatalf("want name New Project, got %q", got.Name)
	}
}

func TestAppsCreate_IncludesPlatformConfig(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/projects/proj/apps" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var err error
		body, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"app","name":"iOS","type":"app_store","project_id":"proj","object":"app","created_at":1}`))
	}))
	t.Cleanup(srv.Close)

	c := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	secret := "1234567890abcdef1234567890abcdef"
	if _, err := c.Apps.Create(context.Background(), "proj", api.AppCreate{
		Name: "iOS",
		Type: "app_store",
		AppStore: &api.AppStoreAppConfig{
			BundleID:     "com.example.app",
			SharedSecret: &secret,
		},
	}); err != nil {
		t.Fatal(err)
	}

	var got struct {
		Name     string `json:"name"`
		Type     string `json:"type"`
		AppStore struct {
			BundleID     string `json:"bundle_id"`
			SharedSecret string `json:"shared_secret"`
		} `json:"app_store"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "iOS" || got.Type != "app_store" || got.AppStore.BundleID != "com.example.app" || got.AppStore.SharedSecret != secret {
		t.Fatalf("unexpected body: %s", string(body))
	}
}

func TestOfferingsUpdate_SetsCurrent(t *testing.T) {
	var body struct {
		IsCurrent *bool `json:"is_current"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/projects/proj/offerings/ofrng" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"ofrng","lookup_key":"default","display_name":"Default","is_current":true,"state":"active","object":"offering","created_at":1,"metadata":null}`)
	}))
	t.Cleanup(srv.Close)

	c := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	current := true
	offering, err := c.Offerings.Update(context.Background(), "proj", "ofrng", api.OfferingUpdate{IsCurrent: &current})
	if err != nil {
		t.Fatal(err)
	}
	if body.IsCurrent == nil || !*body.IsCurrent || !offering.IsCurrent {
		t.Fatalf("current offering not preserved: body=%v response=%+v", body.IsCurrent, offering)
	}
}
