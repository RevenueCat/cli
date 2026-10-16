package cli

import (
	"encoding/json"
	"testing"
)

func TestExperimentPlacementsAllowClearingAndConfiguredOverrides(t *testing.T) {
	for _, raw := range []string{
		`null`, `{}`, `{"placement_offerings":[]}`,
		`{"fallback_offering_a_id":null,"placement_offerings":[]}`,
		`{"fallback_offering_a_id":"ofrng_a","placement_offerings":[{"placement_identifier":"onboarding","offering_a_id":"ofrng_a"}]}`,
	} {
		t.Run(raw, func(t *testing.T) {
			if err := validExperimentPlacements(json.RawMessage(raw)); err != nil {
				t.Fatal(err)
			}
		})
	}
}
