package cli

import (
	"strings"
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

func TestPaywallPickerLabel(t *testing.T) {
	const created = 1700000000000
	pub := api.Millis(created)

	named := paywallPickerLabel(api.Paywall{ID: "pw1", Name: "Hero", OfferingID: "ofrng_x", CreatedAt: created})
	for _, want := range []string{"Hero", "ofrng_x", "draft"} {
		if !strings.Contains(named, want) {
			t.Fatalf("named label %q missing %q", named, want)
		}
	}

	unnamed := paywallPickerLabel(api.Paywall{ID: "pw2", CreatedAt: created, PublishedAt: &pub})
	for _, want := range []string{"pw2", "standalone", "published"} {
		if !strings.Contains(unnamed, want) {
			t.Fatalf("unnamed label %q missing %q", unnamed, want)
		}
	}

	if named == unnamed {
		t.Fatal("distinct paywalls must produce distinct labels")
	}
}
