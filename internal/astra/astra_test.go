package astra

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStreamDecodesEvents(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/editor/v1/stream" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok_123" {
			t.Errorf("Authorization = %q", got)
		}
		if _, err := r.Cookie("rc_auth_token"); err == nil {
			t.Error("rc_auth_token cookie must not be sent; it shadows bearer auth")
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: run.started\ndata: {\"type\":\"run.started\",\"session_id\":\"sess1\"}\n\n")
		io.WriteString(w, "data: {\"type\":\"turn.snapshot\",\"session_id\":\"sess1\",\"turn_index\":0,\"paywall\":{\"default_locale\":\"en_US\",\"offering_id\":\"ofrng1\",\"components_config\":{\"a\":1},\"components_localizations\":{}},\"activity\":[{\"id\":\"a1\",\"type\":\"tool\",\"tool_call_id\":\"tc1\",\"tool_name\":\"edit_components\",\"status\":\"success\",\"display\":{\"text\":\"Edited hero\"}}]}\n\n")
		io.WriteString(w, "data: {\"type\":\"run.completed\",\"session_id\":\"sess1\",\"trace_id\":\"tr1\",\"paywall\":{\"default_locale\":\"en_US\",\"offering_id\":null,\"components_config\":{},\"components_localizations\":{}},\"activity\":[],\"result_screenshots\":[{\"color_scheme\":\"light\",\"mime_type\":\"image/png\",\"data_base64\":\"UE5H\"}]}\n\n")
	}))
	defer server.Close()

	client := NewClient(Options{BaseURL: server.URL, Token: "tok_123"})
	revision := 3
	stream, err := client.Stream(context.Background(), EditorRequest{
		ProjectID: "proj1",
		PaywallID: "pw1",
		Revision:  &revision,
		Paywall: PaywallData{
			DefaultLocale:           "en_US",
			ComponentsConfig:        json.RawMessage(`{}`),
			ComponentsLocalizations: json.RawMessage(`{}`),
		},
		UIConfig:         json.RawMessage(`{}`),
		ProductVariables: map[string]string{},
		Message:          "make it blue",
		SessionItems:     json.RawMessage(`null`),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	started, err := stream.Next()
	if err != nil || started.Type != EventRunStarted || started.SessionID != "sess1" {
		t.Fatalf("started = %+v, err %v", started, err)
	}
	snapshot, err := stream.Next()
	if err != nil || snapshot.Type != EventTurnSnapshot {
		t.Fatalf("snapshot = %+v, err %v", snapshot, err)
	}
	if snapshot.Paywall == nil || *snapshot.Paywall.OfferingID != "ofrng1" {
		t.Fatalf("paywall = %+v", snapshot.Paywall)
	}
	if len(snapshot.Activity) != 1 || snapshot.Activity[0].Display.Text != "Edited hero" {
		t.Fatalf("activity = %+v", snapshot.Activity)
	}
	completed, err := stream.Next()
	if err != nil || !completed.Terminal() || completed.TraceID != "tr1" {
		t.Fatalf("completed = %+v, err %v", completed, err)
	}
	if completed.Paywall.OfferingID != nil {
		t.Fatalf("offering should be null, got %v", completed.Paywall.OfferingID)
	}
	if len(completed.ResultScreenshots) != 1 || completed.ResultScreenshots[0].ColorScheme != "light" || completed.ResultScreenshots[0].DataBase64 != "UE5H" {
		t.Fatalf("result_screenshots = %+v", completed.ResultScreenshots)
	}

	if gotBody["project_id"] != "proj1" || gotBody["paywall_id"] != "pw1" || gotBody["revision"] != 3.0 {
		t.Errorf("body = %v", gotBody)
	}
	if gotBody["message"] != "make it blue" {
		t.Errorf("message = %v", gotBody["message"])
	}
	if _, present := gotBody["__unstable_session_items"]; !present {
		t.Error("__unstable_session_items missing")
	}
}

func TestStreamRunFailed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "data: {\"type\":\"run.failed\",\"session_id\":\"sess1\",\"error\":{\"code\":\"internal\",\"message\":\"boom\"},\"paywall\":null,\"activity\":null}\n\n")
	}))
	defer server.Close()
	client := NewClient(Options{BaseURL: server.URL, Token: "tok"})
	stream, err := client.Stream(context.Background(), EditorRequest{})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	event, err := stream.Next()
	if err != nil || event.Type != EventRunFailed || event.Error.Message != "boom" {
		t.Fatalf("event = %+v, err %v", event, err)
	}
}

func TestStreamHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"detail":"not allowed"}`)
	}))
	defer server.Close()
	client := NewClient(Options{BaseURL: server.URL, Token: "tok"})
	_, err := client.Stream(context.Background(), EditorRequest{})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 403 || apiErr.Message != "not allowed" {
		t.Fatalf("err = %v", err)
	}
}

func TestFeedbackAndRewind(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["session_id"] != "sess1" {
			t.Errorf("session_id = %v", body["session_id"])
		}
	}))
	defer server.Close()
	client := NewClient(Options{BaseURL: server.URL, Token: "tok"})
	if err := client.Feedback(context.Background(), "sess1", "tr1", "good"); err != nil {
		t.Fatal(err)
	}
	if err := client.Rewind(context.Background(), "sess1", "tr1", true); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || paths[0] != "/editor/v1/feedback" || paths[1] != "/editor/v1/rewind" {
		t.Fatalf("paths = %v", paths)
	}
}
