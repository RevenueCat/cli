package cli

import (
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

// paywalls delete is interactive-only (its refusal to run under automation is
// covered by the destructive-command guard), so the force-check is exercised
// directly here: a published or attached paywall needs --force, a standalone
// draft does not.
func TestCheckPaywallDeletable(t *testing.T) {
	published := api.Millis(1700000000000)
	tests := []struct {
		name    string
		paywall *api.Paywall
		force   bool
		wantErr bool
	}{
		{name: "standalone draft deletes", paywall: &api.Paywall{ID: "pw_standalone"}},
		{name: "published needs force", paywall: &api.Paywall{ID: "pw_pub", PublishedAt: &published}, wantErr: true},
		{name: "attached needs force", paywall: &api.Paywall{ID: "pw_att", OfferingID: "ofrng_x"}, wantErr: true},
		{name: "published with force deletes", paywall: &api.Paywall{ID: "pw_pub", PublishedAt: &published}, force: true},
		{name: "attached with force deletes", paywall: &api.Paywall{ID: "pw_att", OfferingID: "ofrng_x"}, force: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkPaywallDeletable(tt.paywall, tt.paywall.ID, tt.force)
			if (err != nil) != tt.wantErr {
				t.Fatalf("checkPaywallDeletable err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
