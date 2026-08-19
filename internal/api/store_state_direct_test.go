package api

import (
	"encoding/json"
	"testing"
)

// Guards against the drift that hid raw_store_status: the live store-state
// response must round-trip into LiveStoreState with every typed field captured.
func TestLiveStoreState_RoundTrip(t *testing.T) {
	const body = `{
	  "project_id": "proj1",
	  "product_id": "prod1",
	  "store": "app_store",
	  "store_status": {"status": "needs_action", "raw_store_status": "MISSING_METADATA"},
	  "common": {
	    "availability": {"territories": {"US": true, "GB": false}},
	    "pricing": {"territory_prices": {"US": {"amount_micros": 9990000, "currency": "USD", "start_date": null}}},
	    "localizations": {"en-US": {"name": "Pro", "description": null}}
	  },
	  "store_state": {"subscription_group_name": "Main"}
	}`

	var s LiveStoreState
	if err := json.Unmarshal([]byte(body), &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if s.StoreStatus == nil || s.StoreStatus.Status != "needs_action" {
		t.Fatalf("store_status.status not captured: %+v", s.StoreStatus)
	}
	if s.StoreStatus.RawStoreStatus == nil || *s.StoreStatus.RawStoreStatus != "MISSING_METADATA" {
		t.Fatalf("raw_store_status not captured: %+v", s.StoreStatus)
	}
	if s.Common == nil || s.Common.Availability == nil || !s.Common.Availability.Territories["US"] || s.Common.Availability.Territories["GB"] {
		t.Fatalf("availability not captured: %+v", s.Common)
	}
	price, ok := s.Common.Pricing.TerritoryPrices["US"]
	if !ok || price.AmountMicros != 9990000 || price.Currency != "USD" {
		t.Fatalf("pricing not captured: %+v", s.Common.Pricing)
	}
	if s.Common.Localizations["en-US"].Name != "Pro" {
		t.Fatalf("localizations not captured: %+v", s.Common.Localizations)
	}
	if s.StoreState["subscription_group_name"] != "Main" {
		t.Fatalf("freeform store_state not captured: %+v", s.StoreState)
	}
}

// A product with no store presence (e.g. Test Store) returns a null
// raw_store_status; it must decode to nil, not the empty string.
func TestLiveStoreState_NullRawStatus(t *testing.T) {
	var s LiveStoreState
	if err := json.Unmarshal([]byte(`{"store_status":{"status":"ok","raw_store_status":null}}`), &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s.StoreStatus.RawStoreStatus != nil {
		t.Fatalf("null raw_store_status should decode to nil, got %q", *s.StoreStatus.RawStoreStatus)
	}
}
