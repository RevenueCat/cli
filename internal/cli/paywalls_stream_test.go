package cli

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/revenuecat/cli/internal/paywallai"
)

func TestApplySessionEvent_PreservesOffering(t *testing.T) {
	off := "ofrng_x"
	session := &paywallAISession{}
	session.Paywall.OfferingID = &off

	applySessionEvent(session, &paywallai.Event{
		SessionID:    "sess1",
		TraceID:      "tr1",
		Paywall:      &paywallai.PaywallData{DefaultLocale: "en_US"}, // OfferingID nil, as the editor echoes it
		SessionItems: json.RawMessage(`[{"k":1}]`),
	})

	if session.SessionID != "sess1" || session.TraceID != "tr1" {
		t.Fatalf("ids not applied: %+v", session)
	}
	if session.Paywall.OfferingID == nil || *session.Paywall.OfferingID != "ofrng_x" {
		t.Fatalf("offering not preserved: %v", session.Paywall.OfferingID)
	}
	if string(session.SessionItems) != `[{"k":1}]` {
		t.Fatalf("session items = %s", session.SessionItems)
	}
}

func TestStreamDropError(t *testing.T) {
	underlying := errors.New("stream ID 1; INTERNAL_ERROR; received from peer")

	saved := streamDropError(paywallAIOptions{sessionPath: "/tmp/s.paywall.json"}, true, underlying)
	if !errors.Is(saved, underlying) {
		t.Fatalf("underlying error not wrapped: %v", saved)
	}
	if h := hintFor(saved); !strings.Contains(h, "edit --session /tmp/s.paywall.json") {
		t.Fatalf("checkpointed hint = %q", h)
	}

	none := streamDropError(paywallAIOptions{}, false, underlying)
	if h := hintFor(none); !strings.Contains(h, "Re-run") {
		t.Fatalf("no-checkpoint hint = %q", h)
	}

	// generate created a draft before the drop: recover via edit, not re-run
	created := streamDropError(paywallAIOptions{sessionPath: "/tmp/s.paywall.json", createdDraft: true}, false, underlying)
	if h := hintFor(created); !strings.Contains(h, "edit --session /tmp/s.paywall.json") {
		t.Fatalf("created-draft hint = %q", h)
	}
}
