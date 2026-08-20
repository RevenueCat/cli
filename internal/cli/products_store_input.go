package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/tui"
)

type storeStateInputOptions struct {
	file                  string
	inputFormat           string
	equalizeBaseTerritory string
}

func (o *storeStateInputOptions) addFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.StringVarP(&o.file, "file", "f", "", "desired state file, or - for stdin (env: RC_STORE_STATE_FILE)")
	flags.StringVar(&o.inputFormat, "input-format", "", "input format for stdin or extensionless files: csv or json")
	flags.StringVar(&o.equalizeBaseTerritory, "equalize-base-territory", "", "equalize missing subscription prices from this base territory (e.g. US)")
}

func (o *storeStateInputOptions) desiredStates(rt *Runtime, app *api.App, stdin io.Reader) ([]api.StoreStatePlanDesiredState, error) {
	states, err := o.gatherDesiredStates(rt, app, stdin)
	if err != nil {
		return nil, err
	}
	if base := strings.ToUpper(strings.TrimSpace(o.equalizeBaseTerritory)); base != "" {
		for i := range states {
			injectEqualizeBaseTerritory(&states[i], base)
		}
	}
	return states, nil
}

// injectEqualizeBaseTerritory adds an equalization directive under
// common.pricing without disturbing any territory_prices already set there.
func injectEqualizeBaseTerritory(state *api.StoreStatePlanDesiredState, base string) {
	if state.Common == nil {
		state.Common = map[string]any{}
	}
	pricing, ok := state.Common["pricing"].(map[string]any)
	if !ok {
		pricing = map[string]any{}
		state.Common["pricing"] = pricing
	}
	pricing["equalize_missing_subscription_prices"] = map[string]any{"base_territory": base}
}

func (o *storeStateInputOptions) gatherDesiredStates(rt *Runtime, app *api.App, stdin io.Reader) ([]api.StoreStatePlanDesiredState, error) {
	if o.file == "" {
		o.file = os.Getenv("RC_STORE_STATE_FILE")
	}
	if o.file == "" {
		if !rt.CanPrompt() {
			return nil, errors.New("desired state input is required: pass --file <path>, pipe input with --file -, or run interactively")
		}
		return promptStoreState(app)
	}

	format := strings.ToLower(strings.TrimSpace(o.inputFormat))
	if format == "" && o.file != "-" {
		format = strings.TrimPrefix(strings.ToLower(filepath.Ext(o.file)), ".")
	}
	if format == "" {
		format = "csv"
	}
	if format != "csv" && format != "json" {
		return nil, fmt.Errorf("--input-format must be csv or json, got %q", format)
	}

	var input io.Reader
	if o.file == "-" {
		input = stdin
	} else {
		f, err := os.Open(o.file)
		if err != nil {
			return nil, fmt.Errorf("open store-state input: %w", err)
		}
		defer f.Close()
		input = f
	}
	if format == "csv" {
		return readStoreStateCSVReader(input, app.ID)
	}
	return readStoreStateJSON(input, app.ID)
}

func readStoreStateJSON(input io.Reader, appID string) ([]api.StoreStatePlanDesiredState, error) {
	data, err := io.ReadAll(input)
	if err != nil {
		return nil, fmt.Errorf("read store-state JSON: %w", err)
	}
	var envelope api.StoreStatePlanCreate
	if err := json.Unmarshal(data, &envelope); err != nil || len(envelope.DesiredStates) == 0 {
		var states []api.StoreStatePlanDesiredState
		if arrayErr := json.Unmarshal(data, &states); arrayErr != nil {
			if err != nil {
				return nil, fmt.Errorf("parse store-state JSON: %w", err)
			}
			return nil, errors.New("store-state JSON must contain a non-empty desired_states array")
		}
		envelope.DesiredStates = states
	}
	if len(envelope.DesiredStates) == 0 {
		return nil, errors.New("store-state JSON must contain at least one desired state")
	}
	for i := range envelope.DesiredStates {
		state := &envelope.DesiredStates[i]
		if state.Store != "app_store" && state.Store != "play_store" {
			return nil, fmt.Errorf("desired_states[%d].store must be app_store or play_store", i)
		}
		if state.ProductID == "" && state.CreateRevenueCatProduct == nil {
			return nil, fmt.Errorf("desired_states[%d] requires product_id or create_revenuecat_product", i)
		}
		if create := state.CreateRevenueCatProduct; create != nil {
			if create.AppID == "" {
				create.AppID = appID
			}
			if create.AppID != appID {
				return nil, fmt.Errorf("desired_states[%d] targets app %s, but command targets %s", i, create.AppID, appID)
			}
		}
	}
	return envelope.DesiredStates, nil
}

func promptStoreState(app *api.App) ([]api.StoreStatePlanDesiredState, error) {
	states := make([]api.StoreStatePlanDesiredState, 0, 1)
	for {
		var identifier, productType, displayName, title, duration string
		var territory, amount, currency, locale, localizedName, localizedDescription string
		if err := tui.Form(false).
			Field(huh.NewInput().Title("Store product identifier").Value(&identifier).Validate(tui.Required("store product identifier"))).
			Field(huh.NewInput().Title("RevenueCat product type").Description("subscription, consumable, non_consumable, non_renewing_subscription, or one_time").Value(&productType).Validate(tui.Required("product type"))).
			Field(huh.NewInput().Title("Display name").Value(&displayName).Validate(tui.Required("display name"))).
			Field(huh.NewInput().Title("Store title").Value(&title).Validate(tui.Required("title"))).
			Field(huh.NewInput().Title("Duration (subscriptions, e.g. P1M)").Value(&duration)).
			Field(huh.NewInput().Title("Initial territory (e.g. US)").Value(&territory)).
			Field(huh.NewInput().Title("Price amount (e.g. 9.99)").Value(&amount)).
			Field(huh.NewInput().Title("Currency (e.g. USD)").Value(&currency)).
			Field(huh.NewInput().Title("Localization locale (e.g. en-US)").Value(&locale)).
			Field(huh.NewInput().Title("Localized name").Value(&localizedName)).
			Field(huh.NewInput().Title("Localized description").Value(&localizedDescription)).
			Run(); err != nil {
			return nil, err
		}
		common := map[string]any{"title": title}
		if duration != "" {
			common["duration"] = duration
		}
		if territory != "" || amount != "" || currency != "" {
			if territory == "" || amount == "" || currency == "" {
				return nil, errors.New("territory, price amount, and currency must be provided together")
			}
			micros, err := decimalToMicros(amount)
			if err != nil {
				return nil, fmt.Errorf("invalid price amount: %w", err)
			}
			common["pricing"] = map[string]any{"territory_prices": map[string]any{
				strings.ToUpper(territory): map[string]any{"amount_micros": micros, "currency": strings.ToUpper(currency)},
			}}
		}
		if locale != "" || localizedName != "" || localizedDescription != "" {
			if locale == "" || localizedName == "" {
				return nil, errors.New("localization locale and localized name must be provided together")
			}
			common["localizations"] = map[string]any{locale: map[string]any{
				"name": localizedName, "description": localizedDescription,
			}}
		}
		store := "app_store"
		var storeState map[string]any
		if app.Type == "play_store" {
			store = "play_store"
			parts := strings.SplitN(identifier, ":", 2)
			if productType == "subscription" {
				if len(parts) != 2 || duration == "" {
					return nil, errors.New("a Play Store subscription requires identifier <subscription_id>:<base_plan_id> and duration")
				}
				if locale == "" {
					return nil, errors.New("a Play Store subscription requires a localization")
				}
				storeState = map[string]any{"base_plans": map[string]any{
					parts[1]: map[string]any{"auto_renewing_base_plan_type": map[string]any{"billing_period_duration": duration}},
				}}
			}
		}
		states = append(states, api.StoreStatePlanDesiredState{
			Store: store,
			CreateRevenueCatProduct: &api.StoreStatePlanProductCreate{
				AppID: app.ID, StoreIdentifier: identifier, Type: productType,
				DisplayName: displayName, Title: title,
			},
			Common: common, StoreState: storeState,
		})
		another, err := tui.ConfirmDefault(false, "Add another product?", false)
		if err != nil {
			return nil, err
		}
		if !another {
			return states, nil
		}
	}
}
