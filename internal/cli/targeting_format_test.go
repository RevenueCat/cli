package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTargetingConfigSummaryFormatsActivationPreview(t *testing.T) {
	conditions := json.RawMessage(`[{"context":null,"field":"country","operator":"in","value":["US"]}]`)
	got, err := targetingConfigSummary("conditions", conditions)
	if err != nil || got != "country in US" {
		t.Fatalf("conditions: got %q, err %v", got, err)
	}
	var responseConditions []any
	if err := json.Unmarshal(conditions, &responseConditions); err != nil {
		t.Fatal(err)
	}
	got, err = targetingConditionsSummary(responseConditions)
	if err != nil || got != "country in US" {
		t.Fatalf("response conditions: got %q, err %v", got, err)
	}
	placements := json.RawMessage(`{"fallback_offering_id":"ofrng_default","placement_offerings":[{"placement_identifier":"onboarding","offering_id":"ofrng_us"}]}`)
	got, err = targetingConfigSummary("placements", placements)
	if err != nil || !strings.Contains(got, "onboarding") || !strings.Contains(got, "ofrng_us") || strings.ContainsAny(got, "{}[]\"") {
		t.Fatalf("placements: got %q, err %v", got, err)
	}
}
