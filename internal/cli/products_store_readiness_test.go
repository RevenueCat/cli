package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/output"
)

func strPtr(s string) *string { return &s }

func liveState(status string, raw *string, territories map[string]bool, priced map[string]api.TerritoryPrice) *api.LiveStoreState {
	state := &api.LiveStoreState{
		Store:       "app_store",
		StoreStatus: &api.StoreStatus{Status: status, RawStoreStatus: raw},
		Common:      &api.StoreStateCommon{},
	}
	if territories != nil {
		state.Common.Availability = &struct {
			Territories map[string]bool `json:"territories"`
		}{Territories: territories}
	}
	if priced != nil {
		state.Common.Pricing = &struct {
			TerritoryPrices map[string]api.TerritoryPrice `json:"territory_prices"`
		}{TerritoryPrices: priced}
	}
	return state
}

func TestClassifyProductReadiness(t *testing.T) {
	usPriced := map[string]api.TerritoryPrice{"US": {AmountMicros: 9990000, Currency: "USD"}}
	tests := []struct {
		name         string
		state        *api.LiveStoreState
		applyStatus  *string
		wantVerdict  readinessVerdict
		wantUnpriced []string
	}{
		{
			name:        "approved and priced is ready",
			state:       liveState("ok", strPtr("APPROVED"), map[string]bool{"US": true}, usPriced),
			wantVerdict: readinessReady,
		},
		{
			name:         "approved with unpriced available territory is incomplete",
			state:        liveState("ok", strPtr("APPROVED"), map[string]bool{"US": true, "GB": true}, usPriced),
			wantVerdict:  readinessIncomplete,
			wantUnpriced: []string{"GB"},
		},
		{
			name:        "missing metadata is incomplete",
			state:       liveState("needs_action", strPtr("MISSING_METADATA"), nil, nil),
			wantVerdict: readinessIncomplete,
		},
		{
			name:        "waiting for review is in progress",
			state:       liveState("needs_action", strPtr("WAITING_FOR_REVIEW"), nil, nil),
			wantVerdict: readinessInProgress,
		},
		{
			name:        "waiting for upload is in progress",
			state:       liveState("needs_action", strPtr("WAITING_FOR_UPLOAD"), nil, nil),
			wantVerdict: readinessInProgress,
		},
		{
			name:        "not found status is failed",
			state:       liveState("not_found", nil, nil, nil),
			wantVerdict: readinessFailed,
		},
		{
			name:        "nil raw with ok status is ready",
			state:       liveState("ok", nil, nil, nil),
			wantVerdict: readinessReady,
		},
		{
			name:        "nil raw with needs_action is incomplete",
			state:       liveState("needs_action", nil, nil, nil),
			wantVerdict: readinessIncomplete,
		},
		{
			name:        "normalized action_in_progress is in progress (Play)",
			state:       liveState("action_in_progress", nil, nil, nil),
			wantVerdict: readinessInProgress,
		},
		{
			name:        "normalized draft is incomplete (Play base plan not active)",
			state:       liveState("draft", nil, nil, nil),
			wantVerdict: readinessIncomplete,
		},
		{
			name:        "normalized inactive_in_store is incomplete (Play)",
			state:       liveState("inactive_in_store", nil, nil, nil),
			wantVerdict: readinessIncomplete,
		},
		{
			name:        "normalized could_not_check is unknown",
			state:       liveState("could_not_check", nil, nil, nil),
			wantVerdict: readinessUnknown,
		},
		{
			name:        "apply status failed is failed",
			state:       liveState("ok", strPtr("APPROVED"), nil, nil),
			applyStatus: strPtr("failed"),
			wantVerdict: readinessFailed,
		},
		{
			name:        "unknown raw falls back to normalized status",
			state:       liveState("ok", strPtr("SOME_FUTURE_STATE"), nil, nil),
			wantVerdict: readinessReady,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyProductReadiness("prod", tt.state, tt.applyStatus)
			if got.Verdict != tt.wantVerdict {
				t.Fatalf("verdict = %s, want %s", got.Verdict, tt.wantVerdict)
			}
			if strings.Join(got.UnpricedTerritories, ",") != strings.Join(tt.wantUnpriced, ",") {
				t.Fatalf("unpriced = %v, want %v", got.UnpricedTerritories, tt.wantUnpriced)
			}
		})
	}
}

func TestReadinessNextActionsAndWarnings(t *testing.T) {
	priced := map[string]api.TerritoryPrice{"US": {AmountMicros: 1, Currency: "USD"}}

	unpriced := classifyProductReadiness("prod", liveState("ok", strPtr("APPROVED"), map[string]bool{"US": true, "GB": true}, priced), nil)
	if !anyContains(unpriced.NextActions, "--equalize-base-territory") {
		t.Errorf("unpriced product should suggest --equalize-base-territory, got %v", unpriced.NextActions)
	}

	missing := classifyProductReadiness("prod", liveState("needs_action", strPtr("MISSING_METADATA"), nil, nil), nil)
	if !anyContains(missing.NextActions, "rc products store screenshot") {
		t.Errorf("missing metadata should mention the screenshot command, got %v", missing.NextActions)
	}

	// A Play product must not be told to use the App Store-only equalize flag.
	playState := liveState("ok", nil, map[string]bool{"US": true, "GB": true}, map[string]api.TerritoryPrice{"US": {AmountMicros: 1, Currency: "USD"}})
	playState.Store = "play_store"
	play := classifyProductReadiness("prod", playState, nil)
	if anyContains(play.NextActions, "--equalize-base-territory") {
		t.Errorf("Play product should not suggest the App Store equalize flag, got %v", play.NextActions)
	}
	if !anyContains(play.NextActions, "usd_price") {
		t.Errorf("Play product should get Play-specific pricing guidance, got %v", play.NextActions)
	}

	ready := classifyProductReadiness("prod", liveState("ok", strPtr("APPROVED"), map[string]bool{"US": true}, priced), nil)
	if len(ready.NextActions) != 0 {
		t.Errorf("ready product should have no next actions, got %v", ready.NextActions)
	}

	st := liveState("needs_action", strPtr("MISSING_METADATA"), nil, nil)
	st.Warnings = []string{"Incomplete territory pricing"}
	if got := classifyProductReadiness("prod", st, nil); len(got.Warnings) != 1 || got.Warnings[0] != "Incomplete territory pricing" {
		t.Errorf("live store warnings should be surfaced verbatim, got %v", got.Warnings)
	}
}

func anyContains(list []string, sub string) bool {
	for _, s := range list {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func TestWorstReadiness(t *testing.T) {
	products := []productReadiness{
		{Verdict: readinessReady},
		{Verdict: readinessInProgress},
		{Verdict: readinessIncomplete},
	}
	if got := worstReadiness(products); got != readinessIncomplete {
		t.Fatalf("worst = %s, want INCOMPLETE", got)
	}
	products = append(products, productReadiness{Verdict: readinessFailed})
	if got := worstReadiness(products); got != readinessFailed {
		t.Fatalf("worst = %s, want FAILED", got)
	}
	if got := worstReadiness(nil); got != readinessUnknown {
		t.Fatalf("worst of none = %s, want UNKNOWN", got)
	}
}

// A create apply can return plan items whose RevenueCat product ID is still
// unset. Those items must be recorded as UNKNOWN (unverifiable), never dropped,
// so a create-only apply never reports READY without a live check.
func TestVerifyStoreStateReadiness_CreateWithNilProductID(t *testing.T) {
	rt := &Runtime{Globals: &Globals{NoInput: true}, Out: output.NewRenderer(io.Discard, io.Discard, false, true, false, "")}
	reader := readerFunc(func(ctx context.Context, projectID, productID string) (*api.LiveStoreState, error) {
		t.Fatalf("live state must not be read for a nil product ID, got %q", productID)
		return nil, nil
	})
	plan := &api.StoreStatePlan{PlanItems: []api.StoreStatePlanItem{
		{ProductID: nil, StoreIdentifier: strPtr("com.example.pro"), Action: "create", ApplyStatus: strPtr("applied")},
	}}
	report := verifyStoreStateReadiness(context.Background(), rt, reader, "proj", plan)
	if report.Overall != readinessUnknown {
		t.Fatalf("overall = %s, want UNKNOWN", report.Overall)
	}
	if len(report.Products) != 1 || report.Products[0].Verdict != readinessUnknown {
		t.Fatalf("products = %+v, want one UNKNOWN item", report.Products)
	}
	if report.Products[0].ProductID != "com.example.pro" {
		t.Fatalf("product display id = %q, want the store identifier", report.Products[0].ProductID)
	}
}

type readerFunc func(ctx context.Context, projectID, productID string) (*api.LiveStoreState, error)

func (f readerFunc) Get(ctx context.Context, projectID, productID string) (*api.LiveStoreState, error) {
	return f(ctx, projectID, productID)
}

func TestDesiredStates_EqualizeBaseTerritory(t *testing.T) {
	input := `{"desired_states":[
		{"store":"app_store","create_revenuecat_product":{"store_identifier":"a","type":"subscription","display_name":"A","title":"A"},"common":{"duration":"P1M"}},
		{"store":"app_store","create_revenuecat_product":{"store_identifier":"b","type":"subscription","display_name":"B","title":"B"},"common":{"pricing":{"territory_prices":{"US":{"amount_micros":9990000,"currency":"USD"}}}}}
	]}`
	opts := storeStateInputOptions{file: "-", inputFormat: "json", equalizeBaseTerritory: "us"}
	rt := &Runtime{Globals: &Globals{NoInput: true}}
	states, err := opts.desiredStates(rt, &api.App{ID: "app", Type: "app_store"}, strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 2 {
		t.Fatalf("expected 2 states, got %d", len(states))
	}
	for i, state := range states {
		pricing, ok := state.Common["pricing"].(map[string]any)
		if !ok {
			t.Fatalf("state %d missing pricing map", i)
		}
		eq, ok := pricing["equalize_missing_subscription_prices"].(map[string]any)
		if !ok {
			t.Fatalf("state %d missing equalize directive", i)
		}
		if eq["base_territory"] != "US" {
			t.Fatalf("state %d base_territory = %v, want US", i, eq["base_territory"])
		}
	}
	// Existing territory_prices under pricing must survive the injection.
	if _, ok := states[1].Common["pricing"].(map[string]any)["territory_prices"]; !ok {
		t.Fatal("existing territory_prices was clobbered")
	}
}

func TestProductsStoreApply_ReportsIncompleteReadiness(t *testing.T) {
	planJSON := `{"id":"plan_123","object":"product_store_state_plan","status":%q,"has_changes":true,"actions":["apply","discard"],"summary":{"products_added":0,"products_modified":1,"products_unchanged":0},"desired_states":[],"plan_items":[{"product_id":"prod_1","app_id":"app","store_identifier":"com.example.pro","action":"update","diff":[],"warnings":[],"error_message":null,"apply_status":%q,"apply_error_message":null}],"error_message":null,"warnings":[]}`
	getCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/projects/proj/store_state/plans/plan_123":
			getCount++
			if getCount == 1 {
				_, _ = io.WriteString(w, `{"id":"plan_123","object":"product_store_state_plan","status":"planned","has_changes":true,"actions":["apply","discard"],"summary":{"products_added":0,"products_modified":1,"products_unchanged":0},"desired_states":[],"plan_items":[{"product_id":"prod_1","app_id":"app","store_identifier":"com.example.pro","action":"update","diff":[],"warnings":[],"error_message":null,"apply_status":"pending","apply_error_message":null}],"error_message":null,"warnings":[]}`)
			} else {
				_, _ = io.WriteString(w, fmt.Sprintf(planJSON, "applied", "applied"))
			}
		case "/projects/proj/store_state/plans/plan_123/actions/apply":
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"id":"plan_123","object":"product_store_state_plan","status":"apply_queued","polling_url":"/poll"}`)
		case "/projects/proj/products/prod_1/store_state":
			_, _ = io.WriteString(w, `{"project_id":"proj","product_id":"prod_1","store":"app_store","store_status":{"status":"needs_action","raw_store_status":"MISSING_METADATA"},"common":{},"store_state":{}}`)
		default:
			http.Error(w, "unexpected request "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	out, _, err := runStoreLifecycleCommand(t, server.URL, "",
		"products", "store", "apply", "plan_123", "--yes", "--json", "--no-input")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"overall": "INCOMPLETE"`) {
		t.Fatalf("output missing INCOMPLETE readiness: %s", out)
	}
	if !strings.Contains(out, `"raw_store_status": "MISSING_METADATA"`) {
		t.Fatalf("output missing raw store status: %s", out)
	}
}
