package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/config"
	"github.com/revenuecat/cli/internal/output"
)

func TestCurrentDraftRevision(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		want    int
		wantErr bool
	}{
		{"draft revision", `{"id":"pw","components":{"published":null,"draft":{"revision":7}}}`, 7, false},
		{"falls back to published", `{"id":"pw","components":{"published":{"revision":3},"draft":null}}`, 3, false},
		{"no revision errors", `{"id":"pw","components":{"published":null,"draft":null}}`, 0, true},
		{"no components errors", `{"id":"pw"}`, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			client := api.NewClient(api.Options{APIKey: "sk", BaseURL: srv.URL})

			got, err := currentDraftRevision(context.Background(), client, "proj", "pw")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got revision %d", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("revision = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestSeedSessionFromServer(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		want    int
		wantErr bool
	}{
		{"seeds draft revision", `{"id":"pw","components":{"draft":{"revision":7,"components_config":{"c":1},"default_locale":"en_US"}}}`, 7, false},
		{"components but nil revision errors", `{"id":"pw","components":{"draft":{"components_config":{"c":1},"default_locale":"en_US"}}}`, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()

			var stdout, stderr bytes.Buffer
			rt := &Runtime{
				Globals: &Globals{NoInput: true, Version: "test"},
				Config:  &config.Config{APIKey: "sk", ProjectID: "proj", BaseURL: srv.URL},
				Ctx:     context.Background(),
				Out:     output.NewRenderer(&stdout, &stderr, true, true, false, ""),
				client:  api.NewClient(api.Options{APIKey: "sk", BaseURL: srv.URL}),
			}

			session, err := seedSessionFromServer(context.Background(), rt, "proj", "pw")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got session with revision %v", session.Revision)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if session.Revision == nil || *session.Revision != tc.want {
				t.Fatalf("revision = %v, want %d", session.Revision, tc.want)
			}
		})
	}
}
