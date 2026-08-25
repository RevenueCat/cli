package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestActiveEntitlementIDs(t *testing.T) {
	future := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	past := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)

	raw := json.RawMessage(`{
		"subscriber": {
			"entitlements": {
				"premium":  {"expires_date": "` + future + `"},
				"expired":  {"expires_date": "` + past + `"},
				"lifetime": {"expires_date": null},
				"garbage":  {"expires_date": "not-a-date"}
			}
		}
	}`)

	got := activeEntitlementIDs(raw)
	want := []string{"lifetime", "premium"} // sorted; expired + garbage excluded
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("activeEntitlementIDs = %v, want %v", got, want)
	}
}

func TestActiveEntitlementIDs_Malformed(t *testing.T) {
	// Non-object / missing subscriber → empty, never a panic, and must encode
	// as a JSON array (`[]`) rather than `null` so `--json | jq` stays safe.
	for _, in := range []string{`"just a string"`, `{}`, `{"subscriber":{}}`, `not json`} {
		got := activeEntitlementIDs(json.RawMessage(in))
		if len(got) != 0 {
			t.Errorf("activeEntitlementIDs(%q) = %v, want empty", in, got)
		}
		if b, _ := json.Marshal(got); string(b) != "[]" {
			t.Errorf("activeEntitlementIDs(%q) marshals to %s, want []", in, b)
		}
	}
}
