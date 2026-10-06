package cli

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/revenuecat/cli/internal/api"
	"github.com/spf13/cobra"
)

func TestExperimentPickerIncludesNextPage(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Query().Get("limit") != "100" {
			t.Errorf("limit=%s", r.URL.Query().Get("limit"))
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("starting_after") == "" {
			fmt.Fprint(w, `{"items":[{"id":"exp1","display_name":"First","status":"draft"}],"next_page":"/projects/proj/experiments?starting_after=exp1"}`)
		} else {
			if r.URL.Query().Get("starting_after") != "exp1" {
				t.Errorf("cursor=%s", r.URL.RawQuery)
			}
			fmt.Fprint(w, `{"items":[{"id":"exp2","display_name":"Second","status":"running"}],"next_page":null}`)
		}
	}))
	defer srv.Close()
	client := api.NewClient(api.Options{BaseURL: srv.URL, APIKey: "sk_test"})
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	items, err := experimentPickerItems(cmd, client, "proj")
	if err != nil || requests != 2 || len(items) != 2 || items[1].ID != "exp2" {
		t.Fatalf("items=%v requests=%d err=%v", items, requests, err)
	}
}
