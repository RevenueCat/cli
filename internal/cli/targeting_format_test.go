package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/revenuecat/cli/internal/api"
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

func TestTargetingAudienceAfterUpdateRequiresClearingOtherField(t *testing.T) {
	audience := "aud_us"
	conditions := []any{map[string]any{"field": "country", "operator": "in", "value": []any{"US"}}}
	for _, tc := range []struct {
		name    string
		current *api.TargetingRule
		body    api.TargetingRuleUpdate
		want    string
		fail    bool
	}{
		{"conditions with existing audience", &api.TargetingRule{AudienceID: &audience}, api.TargetingRuleUpdate{"conditions": json.RawMessage(`[ {"field":"country","operator":"in","value":["US"]} ]`)}, "", true},
		{"audience with existing conditions", &api.TargetingRule{Conditions: conditions}, api.TargetingRuleUpdate{"audience_id": json.RawMessage(`"aud_us"`)}, "", true},
		{"clear audience before conditions", &api.TargetingRule{AudienceID: &audience}, api.TargetingRuleUpdate{"audience_id": json.RawMessage(`null`), "conditions": json.RawMessage(`[ {"field":"country","operator":"in","value":["US"]} ]`)}, "country in US", false},
		{"clear conditions before audience", &api.TargetingRule{Conditions: conditions}, api.TargetingRuleUpdate{"audience_id": json.RawMessage(`"aud_us"`), "conditions": json.RawMessage(`[]`)}, "aud_us", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, _, err := targetingAudienceAfterUpdate(tc.current, tc.body)
			if (err != nil) != tc.fail || (!tc.fail && got != tc.want) {
				t.Fatalf("audience=%q err=%v, want %q fail=%v", got, err, tc.want, tc.fail)
			}
		})
	}
}
