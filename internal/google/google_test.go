package google

import (
	"errors"
	"regexp"
	"testing"

	cloudresourcemanager "google.golang.org/api/cloudresourcemanager/v3"
	"google.golang.org/api/googleapi"
)

func TestParseDeveloperID(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"https://play.google.com/console/u/0/developers/5412345678901234567/app-list", "5412345678901234567", false},
		{"https://play.google.com/console/developers/5412345678901234567/", "5412345678901234567", false},
		{"5412345678901234567", "5412345678901234567", false},
		{"  5412345678901234567  ", "5412345678901234567", false},
		{"not-an-id", "", true},
		{"", "", true},
	}
	for _, c := range cases {
		got, err := ParseDeveloperID(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseDeveloperID(%q): expected error, got %q", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseDeveloperID(%q): unexpected error %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("ParseDeveloperID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestProjectIDFromName(t *testing.T) {
	valid := regexp.MustCompile(`^[a-z][a-z0-9-]{4,28}[a-z0-9]$`)
	for _, name := range []string{"RevenueCat Play Test", "My App!!", "123 numbers first", "x", "A very long display name that exceeds the limit by quite a lot"} {
		got := ProjectIDFromName(name)
		if len(got) < 6 || len(got) > 30 {
			t.Errorf("ProjectIDFromName(%q) = %q: length %d out of 6..30", name, got, len(got))
		}
		if !valid.MatchString(got) {
			t.Errorf("ProjectIDFromName(%q) = %q: not a valid project ID", name, got)
		}
	}
	// Different calls should differ (random suffix). Assign first — comparing
	// the two calls inline reads as an identical-expression bug to staticcheck
	// (SA4000), which can't see that the function is non-deterministic.
	first := ProjectIDFromName("same")
	second := ProjectIDFromName("same")
	if first == second {
		t.Error("expected a random suffix to make IDs differ")
	}
}

func TestServiceAccountEmail(t *testing.T) {
	got := ServiceAccountEmail("my-app-prod")
	want := "revenuecat-service-account@my-app-prod.iam.gserviceaccount.com"
	if got != want {
		t.Errorf("ServiceAccountEmail = %q, want %q", got, want)
	}
}

func TestAddMember(t *testing.T) {
	policy := &cloudresourcemanager.Policy{}
	member := "serviceAccount:sa@p.iam.gserviceaccount.com"

	if !addMember(policy, "roles/pubsub.editor", member) {
		t.Fatal("first addMember should report a change")
	}
	if addMember(policy, "roles/pubsub.editor", member) {
		t.Error("second addMember for same role+member should be a no-op")
	}
	if !addMember(policy, "roles/monitoring.viewer", member) {
		t.Error("addMember for a new role should report a change")
	}
	if len(policy.Bindings) != 2 {
		t.Errorf("expected 2 bindings, got %d", len(policy.Bindings))
	}
}

func TestHasAllPermissions(t *testing.T) {
	if !hasAllPermissions(PlayAppPermissions) {
		t.Error("exact permission set should satisfy hasAllPermissions")
	}
	if hasAllPermissions([]string{"CAN_VIEW_NON_FINANCIAL_DATA"}) {
		t.Error("partial permission set should not satisfy hasAllPermissions")
	}
	if !hasAllPermissions(append([]string{"EXTRA"}, PlayAppPermissions...)) {
		t.Error("superset should satisfy hasAllPermissions")
	}
}

func TestClassifyOrgPolicy(t *testing.T) {
	keyErr := &googleapi.Error{Code: 400, Message: "Key creation is not allowed on this service account."}
	var op *OrgPolicyError
	if !errors.As(classifyOrgPolicy(keyErr), &op) || op.Constraint != "iam.disableServiceAccountKeyCreation" {
		t.Errorf("expected key-creation org-policy classification, got %v", classifyOrgPolicy(keyErr))
	}

	plain := errors.New("network blip")
	if classifyOrgPolicy(plain) != plain {
		t.Error("non-API errors should pass through unchanged")
	}
	if classifyOrgPolicy(nil) != nil {
		t.Error("nil should stay nil")
	}
}

func TestParseDeveloperIDGrantPackage(t *testing.T) {
	name := "developers/555/users/sa@p.iam.gserviceaccount.com/grants/com.example.app"
	if got := grantPackage(name); got != "com.example.app" {
		t.Errorf("grantPackage = %q, want com.example.app", got)
	}
	if got := grantPackage("no-grants-here"); got != "" {
		t.Errorf("grantPackage(malformed) = %q, want empty", got)
	}
}
