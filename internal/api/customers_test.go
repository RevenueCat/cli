package api_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

// idsFromFixture reads {project_id, id} out of a customer fixture so the test
// stays robust if scrubbed IDs shift (the scrubber renumbers when the fixture
// set changes; tests should not hardcode the result).
func idsFromFixture(t *testing.T, file string) (projectID, customerID string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "v2", file))
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		ID        string `json:"id"`
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v.ProjectID, v.ID
}

func TestCustomersGet_EmbedsActiveEntitlements(t *testing.T) {
	const file = "projects_PROJ_customers_CUST.json"
	proj, cust := idsFromFixture(t, file)

	srv := fixtureServer(t, map[string]string{
		"GET /projects/" + proj + "/customers/" + cust: file,
	})
	c := api.NewClient(api.Options{APIKey: "sk_test", BaseURL: srv.URL})

	got, err := c.Customers.Get(context.Background(), proj, cust)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != cust {
		t.Errorf("want %s, got %s", cust, got.ID)
	}
	if got.LastSeenCountry != "US" {
		t.Errorf("want country US, got %q", got.LastSeenCountry)
	}
	if got.ActiveEntitlements == nil {
		t.Fatal("want active_entitlements embedded; got nil")
	}
	if got.ActiveEntitlements.Object != "list" {
		t.Errorf("want object=list, got %q", got.ActiveEntitlements.Object)
	}
}
