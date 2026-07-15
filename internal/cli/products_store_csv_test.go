package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadStoreStateCSV_AppStoreCanonicalRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.csv")
	content := `row_type,store,store_identifier,product_type,display_name,title,duration,territory,amount,currency,start_date,available,available_in_new_territories,locale,localized_name,localized_description,app_store_subscription_group_name,app_store_subscription_group_localized_name,app_store_subscription_group_custom_app_name
price,app_store,com.example.pro,subscription,Pro Monthly,Premium,P1M,US,3.99,USD,2026-07-20,true,true,,,,Premium Group,,
localization,app_store,com.example.pro,subscription,Pro Monthly,Premium,P1M,,,,,,,en-US,Premium,Premium subscription,Premium Group,Premium Subscriptions,Example
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	states, err := readStoreStateCSV(path, "app_abc")
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 {
		t.Fatalf("states = %d, want 1", len(states))
	}
	state := states[0]
	if state.Store != "app_store" || state.CreateRevenueCatProduct.AppID != "app_abc" || state.CreateRevenueCatProduct.StoreIdentifier != "com.example.pro" {
		t.Fatalf("unexpected identity: %+v", state)
	}
	pricing := state.Common["pricing"].(map[string]any)
	prices := pricing["territory_prices"].(map[string]any)
	us := prices["US"].(map[string]any)
	if us["amount_micros"] != int64(3_990_000) || us["currency"] != "USD" {
		t.Fatalf("unexpected US price: %+v", us)
	}
	localizations := state.Common["localizations"].(map[string]any)
	if localizations["en-US"].(map[string]any)["name"] != "Premium" {
		t.Fatalf("unexpected localizations: %+v", localizations)
	}
	if state.StoreState["subscription_group_name"] != "Premium Group" {
		t.Fatalf("unexpected store state: %+v", state.StoreState)
	}
}

func TestReadStoreStateCSV_RejectsConflictingProductMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.csv")
	content := `store,store_identifier,product_type,display_name,title
app_store,com.example.pro,subscription,Pro,Premium
app_store,com.example.pro,subscription,Pro,Other
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readStoreStateCSV(path, "app"); err == nil {
		t.Fatal("expected conflicting title error")
	}
}

func TestReadStoreStateCSV_PlayStoreBasePlan(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.csv")
	content := `store,store_identifier,product_type,display_name,title,duration,territory,amount,currency,locale,localized_name,localized_description,play_store_base_plan_type,play_store_auto_renewing_grace_period_duration
play_store,premium:monthly,subscription,Premium,Premium Monthly,P1M,US,9.99,USD,en-US,Premium,Monthly access,auto_renewing,P3D
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	states, err := readStoreStateCSV(path, "app_android")
	if err != nil {
		t.Fatal(err)
	}
	basePlans := states[0].StoreState["base_plans"].(map[string]any)
	monthly := basePlans["monthly"].(map[string]any)
	auto := monthly["auto_renewing_base_plan_type"].(map[string]any)
	if auto["billing_period_duration"] != "P1M" || auto["grace_period_duration"] != "P3D" {
		t.Fatalf("unexpected base plan: %+v", auto)
	}
}

func TestDecimalToMicros(t *testing.T) {
	for input, want := range map[string]int64{"3.99": 3_990_000, "0": 0, "-5": -5_000_000, "1.000001": 1_000_001} {
		got, err := decimalToMicros(input)
		if err != nil || got != want {
			t.Errorf("decimalToMicros(%q) = %d, %v; want %d", input, got, err, want)
		}
	}
}
