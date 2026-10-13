package cli

import (
	"strings"
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

func TestAudienceToItem_SanitizesBrowserText(t *testing.T) {
	a := api.Audience{
		ID:   "aud_1",
		Name: "Pro\x1b[31m users\nfake row",
		Rules: api.AudienceRules{Groups: []api.AudienceRuleGroup{{Conditions: []api.AudienceRuleCondition{
			{Field: "country", Operator: "is", Value: "DE\x1b]8;;https://evil\x07"},
		}}}},
	}

	item := audienceToItem("proj1ab2c3d4", a)

	texts := append([]string{item.Label, item.Meta}, item.Row...)
	for _, f := range item.Fields {
		texts = append(texts, f.Value)
	}
	for _, s := range texts {
		if strings.ContainsAny(s, "\x1b\x07\n") {
			t.Errorf("unsanitized browser text: %q", s)
		}
	}
}
