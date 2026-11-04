package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

func TestStoreSubmitSendsProductIDsAndPreservesOutcomes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/projects/proj/products/actions/submit_to_store" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			Store      string   `json:"store"`
			ProductIDs []string `json:"product_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Store != "app_store" || !reflect.DeepEqual(body.ProductIDs, []string{"prod1", "prod2"}) {
			t.Errorf("unexpected body: %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"submitted_count":1,"results":[{"product_id":"prod1","status":"submitted","submission_id":"sub1","message":null},{"product_id":"prod2","status":"skipped","submission_id":null,"message":"metadata incomplete"}]}`)
	}))
	t.Cleanup(server.Close)
	client := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: server.URL})
	result, err := client.StoreState.SubmitToStore(context.Background(), "proj", "app_store", []string{"prod1", "prod2"})
	if err != nil {
		t.Fatal(err)
	}
	if result.SubmittedCount != 1 || len(result.Results) != 2 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Results[0].SubmissionID == nil || *result.Results[0].SubmissionID != "sub1" || result.Results[1].Message == nil || *result.Results[1].Message != "metadata incomplete" {
		t.Fatalf("lost submission details: %+v", result.Results)
	}
}

func TestStoreSubmitReturnsTypedAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"type":"parameter_error","message":"products must belong to the same app"}`)
	}))
	t.Cleanup(server.Close)
	client := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: server.URL})
	_, err := client.StoreState.SubmitToStore(context.Background(), "proj", "app_store", []string{"prod1", "prod2"})
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest || apiErr.Type != "parameter_error" {
		t.Fatalf("expected typed submission error, got %v", err)
	}
}
