package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

func TestCheckpointsCreate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/projects/proj/checkpoints" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if len(body) != 1 || body["identifier"] != "after_onboarding" {
			t.Errorf("unexpected body: %v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"id":"chkpt1","object":"checkpoint","project_id":"proj","identifier":"after_onboarding","created_via":"api"}`)
	}))
	t.Cleanup(srv.Close)
	client := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})
	checkpoint, err := client.Checkpoints.Create(context.Background(), "proj", api.CreateCheckpointRequest{Identifier: "after_onboarding"})
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.ID != "chkpt1" || checkpoint.Identifier != "after_onboarding" || checkpoint.ProjectID != "proj" || checkpoint.CreatedVia != "api" {
		t.Fatalf("unexpected checkpoint: %+v", checkpoint)
	}
}
