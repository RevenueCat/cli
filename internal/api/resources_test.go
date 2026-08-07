package api_test

import (
	"context"
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

// These tests verify that every resource the CLI uses correctly deserialises
// real API response shapes via the generated types. Each test uses a fixture
// captured from the live API (internal/api/testdata/v2/) so that any
// breaking spec change surfaces as a compile error (wrong field name / type)
// or a test failure (missing required field, unexpected nil).

const (
	projID    = "proj_test_001"
	appID     = "appa_test_001"
	appIDiOS  = "appe_test_001"
	offerID   = "ofrng_test_003"
	productID = "prod_test_002"
	paywallID = "pw_test_001"
)

// --- Apps ---

func TestAppsList_DecodesMultipleStoreTypes(t *testing.T) {
	srv := fixtureServer(t, map[string]string{
		"GET /projects/" + projID + "/apps": "projects_PROJ_apps.json",
	})
	c := newClient(t, srv.URL)
	page, err := c.Apps.List(context.Background(), projID)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("want 2 apps, got %d", len(page.Items))
	}
	rcBilling := page.Items[0]
	if rcBilling.ID != "appa_test_001" {
		t.Errorf("want appa_test_001, got %q", rcBilling.ID)
	}
	if rcBilling.Type != "rc_billing" {
		t.Errorf("want type rc_billing, got %q", rcBilling.Type)
	}
	if rcBilling.RcBilling == nil {
		t.Error("want rc_billing block, got nil")
	}

	appStore := page.Items[1]
	if appStore.Type != "app_store" {
		t.Errorf("want type app_store, got %q", appStore.Type)
	}
	if appStore.AppStore == nil {
		t.Error("want app_store block, got nil")
	}
	if appStore.AppStore.BundleID == "" {
		t.Error("want bundle_id in app_store block")
	}
}

func TestAppsGet_DecodesSingleApp(t *testing.T) {
	srv := fixtureServer(t, map[string]string{
		"GET /projects/" + projID + "/apps/" + appID: "projects_PROJ_apps_APP.json",
	})
	c := newClient(t, srv.URL)
	app, err := c.Apps.Get(context.Background(), projID, appID)
	if err != nil {
		t.Fatal(err)
	}
	if app.ID != appID {
		t.Errorf("want %s, got %s", appID, app.ID)
	}
	if app.CreatedAt == 0 {
		t.Error("created_at not parsed")
	}
}

// --- Offerings ---

func TestOfferingsList_DecodesPage(t *testing.T) {
	srv := fixtureServer(t, map[string]string{
		"GET /projects/" + projID + "/offerings": "projects_PROJ_offerings.json",
	})
	c := newClient(t, srv.URL)
	page, err := c.Offerings.List(context.Background(), projID)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 4 {
		t.Fatalf("want 4 offerings, got %d", len(page.Items))
	}
	current := page.Items[1]
	if !current.IsCurrent {
		t.Error("want is_current=true on second offering")
	}
	if current.LookupKey != "default" {
		t.Errorf("want lookup_key=default, got %q", current.LookupKey)
	}
}

func TestOfferingsGet_DecodesSingle(t *testing.T) {
	srv := fixtureServer(t, map[string]string{
		"GET /projects/" + projID + "/offerings/" + offerID: "projects_PROJ_offerings_OFFER.json",
	})
	c := newClient(t, srv.URL)
	o, err := c.Offerings.Get(context.Background(), projID, offerID)
	if err != nil {
		t.Fatal(err)
	}
	if o.ID != offerID {
		t.Errorf("want %s, got %s", offerID, o.ID)
	}
	if o.State != "active" {
		t.Errorf("want state=active, got %q", o.State)
	}
}

// --- Packages ---

func TestPackagesList_DecodesPage(t *testing.T) {
	srv := fixtureServer(t, map[string]string{
		"GET /projects/" + projID + "/offerings/" + offerID + "/packages": "projects_PROJ_offerings_OFFER_packages.json",
	})
	c := newClient(t, srv.URL)
	page, err := c.Packages.List(context.Background(), projID, offerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 5 {
		t.Fatalf("want 5 packages, got %d", len(page.Items))
	}
	if page.Items[0].LookupKey != "$rc_weekly" {
		t.Errorf("want $rc_weekly, got %q", page.Items[0].LookupKey)
	}
}

// --- Products ---

func TestProductsList_DecodesSubscriptionShape(t *testing.T) {
	srv := fixtureServer(t, map[string]string{
		"GET /projects/" + projID + "/products": "projects_PROJ_products.json",
	})
	c := newClient(t, srv.URL)
	page, err := c.Products.List(context.Background(), projID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 7 {
		t.Fatalf("want 7 products, got %d", len(page.Items))
	}
	p := page.Items[0]
	if p.Type != "subscription" {
		t.Errorf("want type=subscription, got %q", p.Type)
	}
	if p.Subscription == nil {
		t.Fatal("want subscription block, got nil")
	}
	if p.StoreIdentifier != "com.revenuecat.simpleapp.yearly" {
		t.Errorf("unexpected store_identifier: %q", p.StoreIdentifier)
	}
}

func TestProductsGet_DecodesSingle(t *testing.T) {
	srv := fixtureServer(t, map[string]string{
		"GET /projects/" + projID + "/products/" + productID: "projects_PROJ_products_PROD.json",
	})
	c := newClient(t, srv.URL)
	p, err := c.Products.Get(context.Background(), projID, productID)
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != productID {
		t.Errorf("want %s, got %s", productID, p.ID)
	}
	if p.Subscription == nil || p.Subscription.Duration == nil {
		t.Error("want subscription.duration present")
	}
}

// --- Paywalls ---

func TestPaywallsList_DecodesPage(t *testing.T) {
	srv := fixtureServer(t, map[string]string{
		"GET /projects/" + projID + "/paywalls": "projects_PROJ_paywalls.json",
	})
	c := newClient(t, srv.URL)
	page, err := c.Paywalls.List(context.Background(), projID)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("want 2 paywalls, got %d", len(page.Items))
	}
	pw := page.Items[0]
	if pw.ID != paywallID {
		t.Errorf("want %s, got %s", paywallID, pw.ID)
	}
	if pw.OfferingID != offerID {
		t.Errorf("want offering_id %s, got %s", offerID, pw.OfferingID)
	}
	if !pw.AutomaticallyScaleFontSize {
		t.Error("want automatically_scale_font_size=true")
	}
}

// --- Webhooks ---

func TestWebhooksList_DecodesEmptyPage(t *testing.T) {
	srv := fixtureServer(t, map[string]string{
		"GET /projects/" + projID + "/integrations/webhooks": "projects_PROJ_integrations_webhooks.json",
	})
	c := newClient(t, srv.URL)
	page, err := c.Webhooks.List(context.Background(), projID)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 {
		t.Errorf("want empty page, got %d items", len(page.Items))
	}
	if page.Object != "list" {
		t.Errorf("want object=list, got %q", page.Object)
	}
}

// --- Metrics ---

func TestMetricsOverview_DecodesAllMetrics(t *testing.T) {
	srv := fixtureServer(t, map[string]string{
		"GET /projects/" + projID + "/metrics/overview": "projects_PROJ_metrics_overview.json",
	})
	c := newClient(t, srv.URL)
	overview, err := c.Metrics.Overview(context.Background(), projID)
	if err != nil {
		t.Fatal(err)
	}
	if len(overview.Metrics) != 6 {
		t.Fatalf("want 6 metrics, got %d", len(overview.Metrics))
	}
	mrr := overview.Metrics[2]
	if mrr.ID != "mrr" {
		t.Errorf("want id=mrr, got %q", mrr.ID)
	}
	if mrr.Unit != "$" {
		t.Errorf("want unit=$, got %q", mrr.Unit)
	}
}

// --- Audit ---

func TestAuditList_DecodesAdditionalData(t *testing.T) {
	srv := fixtureServer(t, map[string]string{
		"GET /projects/" + projID + "/audit_logs": "projects_PROJ_audit_logs.json",
	})
	c := newClient(t, srv.URL)
	page, err := c.Audit.List(context.Background(), projID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("want 1 audit log, got %d", len(page.Items))
	}
	log := page.Items[0]
	if log.ID != "log_test_001" {
		t.Errorf("want log_test_001, got %q", log.ID)
	}
	if log.ActionType != "api_key_created" {
		t.Errorf("want api_key_created, got %q", log.ActionType)
	}
	if log.ActorType != "user" {
		t.Errorf("want actor_type=user, got %q", log.ActorType)
	}
}

// --- helper ---

func newClient(t *testing.T, baseURL string) *api.Client {
	t.Helper()
	return api.NewClient(api.Options{APIKey: "sk_test", BaseURL: baseURL})
}
