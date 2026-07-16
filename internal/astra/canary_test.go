package astra

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCanaryHeader(t *testing.T) {
	for name, canary := range map[string]string{"set": "my-canary", "unset": ""} {
		t.Run(name, func(t *testing.T) {
			var got string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Get("X-RC-Canary")
				w.Write([]byte(`{}`))
			}))
			defer server.Close()

			client := NewClient(Options{BaseURL: server.URL, Token: "tok", Canary: canary})
			if err := client.Feedback(context.Background(), "sess", "trace", "up"); err != nil {
				t.Fatal(err)
			}
			if got != canary {
				t.Errorf("X-RC-Canary = %q, want %q", got, canary)
			}
		})
	}
}
