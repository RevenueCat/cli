package cli

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/revenuecat/cli/internal/api"
)

var storeCSVRequiredColumns = []string{
	"store", "store_identifier", "product_type", "display_name", "title",
}

type storeCSVProduct struct {
	line            int
	store           string
	storeIdentifier string
	productType     string
	displayName     string
	title           string
	duration        string
	common          map[string]any
	storeState      map[string]any
}

func readStoreStateCSV(path, appID string) ([]api.StoreStatePlanDesiredState, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open store-state CSV: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.TrimLeadingSpace = true
	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("read store-state CSV header: %w", err)
	}
	if len(header) > 0 {
		header[0] = strings.TrimPrefix(header[0], "\ufeff")
	}
	columns := make(map[string]int, len(header))
	for i, name := range header {
		columns[strings.TrimSpace(name)] = i
	}
	for _, name := range storeCSVRequiredColumns {
		if _, ok := columns[name]; !ok {
			return nil, fmt.Errorf("store-state CSV is missing required column %q", name)
		}
	}

	products := map[string]*storeCSVProduct{}
	order := make([]string, 0)
	for line := 2; ; line++ {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read store-state CSV line %d: %w", line, err)
		}
		value := func(name string) string {
			i, ok := columns[name]
			if !ok || i >= len(row) {
				return ""
			}
			return strings.TrimSpace(row[i])
		}
		if emptyCSVRow(row) {
			continue
		}
		store, identifier := value("store"), value("store_identifier")
		if store != "app_store" && store != "play_store" {
			return nil, fmt.Errorf("store-state CSV line %d: store must be app_store or play_store", line)
		}
		if identifier == "" {
			return nil, fmt.Errorf("store-state CSV line %d: store_identifier is required", line)
		}
		key := store + "\x00" + identifier
		product := products[key]
		if product == nil {
			product = &storeCSVProduct{line: line, store: store, storeIdentifier: identifier, common: map[string]any{}, storeState: map[string]any{}}
			products[key] = product
			order = append(order, key)
		}
		for name, target := range map[string]*string{
			"product_type": &product.productType,
			"display_name": &product.displayName,
			"title":        &product.title,
			"duration":     &product.duration,
		} {
			if err := mergeCSVScalar(target, value(name), name, line); err != nil {
				return nil, err
			}
		}
		if err := mergeStoreCSVRow(product, value, line); err != nil {
			return nil, err
		}
	}
	if len(order) == 0 {
		return nil, fmt.Errorf("store-state CSV contains no product rows")
	}

	states := make([]api.StoreStatePlanDesiredState, 0, len(order))
	for _, key := range order {
		p := products[key]
		if p.productType == "" || p.displayName == "" || p.title == "" {
			return nil, fmt.Errorf("store-state CSV product %q: product_type, display_name, and title are required", p.storeIdentifier)
		}
		if p.title != "" {
			p.common["title"] = p.title
		}
		if p.duration != "" {
			p.common["duration"] = p.duration
		}
		state := api.StoreStatePlanDesiredState{
			Store: p.store,
			CreateRevenueCatProduct: &api.StoreStatePlanProductCreate{
				AppID: appID, StoreIdentifier: p.storeIdentifier, Type: p.productType,
				DisplayName: p.displayName, Title: p.title,
			},
			Common: p.common,
		}
		if len(p.storeState) > 0 {
			state.StoreState = p.storeState
		}
		states = append(states, state)
	}
	return states, nil
}

func mergeStoreCSVRow(p *storeCSVProduct, value func(string) string, line int) error {
	territory := strings.ToUpper(value("territory"))
	amount, currency := value("amount"), strings.ToUpper(value("currency"))
	if amount != "" || currency != "" {
		if territory == "" || amount == "" || currency == "" {
			return fmt.Errorf("store-state CSV line %d: territory, amount, and currency must be provided together", line)
		}
		micros, err := decimalToMicros(amount)
		if err != nil {
			return fmt.Errorf("store-state CSV line %d: invalid amount: %w", line, err)
		}
		price := map[string]any{"amount_micros": micros, "currency": currency}
		if startDate := value("start_date"); startDate != "" {
			price["start_date"] = startDate
		}
		pricing := childMap(p.common, "pricing")
		prices := childMap(pricing, "territory_prices")
		if err := mergeCSVMapValue(prices, territory, price, "price", line); err != nil {
			return err
		}
	}
	if available := value("available"); available != "" {
		if territory == "" {
			return fmt.Errorf("store-state CSV line %d: territory is required when available is set", line)
		}
		parsed, err := parseCSVBool(available)
		if err != nil {
			return fmt.Errorf("store-state CSV line %d: available: %w", line, err)
		}
		childMap(childMap(p.common, "availability"), "territories")[territory] = parsed
	}
	if raw := value("available_in_new_territories"); raw != "" {
		parsed, err := parseCSVBool(raw)
		if err != nil {
			return fmt.Errorf("store-state CSV line %d: available_in_new_territories: %w", line, err)
		}
		availability := childMap(p.common, "availability")
		if err := mergeCSVMapValue(availability, "available_in_new_territories", parsed, "available_in_new_territories", line); err != nil {
			return err
		}
	}
	locale := value("locale")
	localizedName, localizedDescription := value("localized_name"), value("localized_description")
	if locale != "" || localizedName != "" || localizedDescription != "" {
		if locale == "" || localizedName == "" {
			return fmt.Errorf("store-state CSV line %d: locale and localized_name must be provided together", line)
		}
		localization := map[string]any{"name": localizedName, "description": nil}
		if localizedDescription != "" {
			localization["description"] = localizedDescription
		}
		if err := mergeCSVMapValue(childMap(p.common, "localizations"), locale, localization, "localization", line); err != nil {
			return err
		}
	}

	if p.store == "app_store" {
		return mergeAppStoreCSVRow(p, value, locale, line)
	}
	return mergePlayStoreCSVRow(p, value, line)
}

func mergeAppStoreCSVRow(p *storeCSVProduct, value func(string) string, locale string, line int) error {
	if groupName := value("app_store_subscription_group_name"); groupName != "" {
		if err := mergeCSVMapValue(p.storeState, "subscription_group_name", groupName, "app_store_subscription_group_name", line); err != nil {
			return err
		}
	}
	name := value("app_store_subscription_group_localized_name")
	customName := value("app_store_subscription_group_custom_app_name")
	if name == "" && customName == "" {
		return nil
	}
	if locale == "" || name == "" {
		return fmt.Errorf("store-state CSV line %d: locale and app_store_subscription_group_localized_name must be provided together", line)
	}
	localization := map[string]any{"name": name}
	if customName != "" {
		localization["custom_app_name"] = customName
	}
	return mergeCSVMapValue(childMap(p.storeState, "subscription_group_localizations"), locale, localization, "subscription group localization", line)
}

func mergePlayStoreCSVRow(p *storeCSVProduct, value func(string) string, line int) error {
	parts := strings.SplitN(p.storeIdentifier, ":", 2)
	planType := value("play_store_base_plan_type")
	playFields := []string{
		"play_store_base_plan_offer_tags",
		"play_store_auto_renewing_grace_period_duration",
		"play_store_auto_renewing_account_hold_duration",
		"play_store_auto_renewing_resubscribe_state",
		"play_store_auto_renewing_proration_mode",
		"play_store_auto_renewing_legacy_compatible",
		"play_store_auto_renewing_legacy_compatible_subscription_offer_id",
		"play_store_prepaid_time_extension",
		"play_store_installments_committed_payments_count",
		"play_store_installments_renewal_type",
		"play_store_installments_grace_period_duration",
		"play_store_installments_account_hold_duration",
		"play_store_installments_resubscribe_state",
		"play_store_installments_proration_mode",
		"play_store_other_regions_available_in_new_territories",
		"play_store_other_regions_usd_amount",
		"play_store_other_regions_usd_currency",
		"play_store_other_regions_eur_amount",
		"play_store_other_regions_eur_currency",
	}
	hasPlayState := planType != ""
	for _, field := range playFields {
		hasPlayState = hasPlayState || value(field) != ""
	}
	if !hasPlayState {
		return nil
	}
	if len(parts) != 2 {
		return fmt.Errorf("store-state CSV line %d: Play Store subscription identifiers must be <subscription_id>:<base_plan_id>", line)
	}
	basePlan := childMap(childMap(p.storeState, "base_plans"), parts[1])
	if planType != "" {
		field := map[string]string{"auto_renewing": "auto_renewing_base_plan_type", "prepaid": "prepaid_base_plan_type", "installments": "installments_base_plan_type"}[planType]
		if field == "" {
			return fmt.Errorf("store-state CSV line %d: invalid play_store_base_plan_type %q", line, planType)
		}
		if p.duration == "" {
			return fmt.Errorf("store-state CSV line %d: duration is required for a Play Store base plan", line)
		}
		config := childMap(basePlan, field)
		config["billing_period_duration"] = p.duration
		prefix := "play_store_" + planType + "_"
		stringFields := map[string]string{
			"grace_period_duration": "grace_period_duration", "account_hold_duration": "account_hold_duration",
			"resubscribe_state": "resubscribe_state", "proration_mode": "proration_mode",
			"legacy_compatible_subscription_offer_id": "legacy_compatible_subscription_offer_id",
			"time_extension": "time_extension", "renewal_type": "renewal_type",
		}
		for suffix, target := range stringFields {
			if raw := value(prefix + suffix); raw != "" {
				config[target] = raw
			}
		}
		if raw := value(prefix + "legacy_compatible"); raw != "" {
			parsed, err := parseCSVBool(raw)
			if err != nil {
				return fmt.Errorf("store-state CSV line %d: %slegacy_compatible: %w", line, prefix, err)
			}
			config["legacy_compatible"] = parsed
		}
		if raw := value(prefix + "committed_payments_count"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil {
				return fmt.Errorf("store-state CSV line %d: committed payments count must be an integer", line)
			}
			config["committed_payments_count"] = parsed
		}
	}
	if raw := value("play_store_base_plan_offer_tags"); raw != "" {
		tags := strings.Split(raw, ";")
		for i := range tags {
			tags[i] = strings.TrimSpace(tags[i])
		}
		basePlan["offer_tags"] = tags
	}
	return mergePlayOtherRegions(basePlan, value, line)
}

func mergePlayOtherRegions(basePlan map[string]any, value func(string) string, line int) error {
	other := map[string]any{}
	if raw := value("play_store_other_regions_available_in_new_territories"); raw != "" {
		parsed, err := parseCSVBool(raw)
		if err != nil {
			return fmt.Errorf("store-state CSV line %d: other regions availability: %w", line, err)
		}
		other["new_subscriber_availability"] = parsed
	}
	for _, currency := range []string{"usd", "eur"} {
		amount := value("play_store_other_regions_" + currency + "_amount")
		code := strings.ToUpper(value("play_store_other_regions_" + currency + "_currency"))
		if amount == "" && code == "" {
			continue
		}
		if amount == "" || code == "" {
			return fmt.Errorf("store-state CSV line %d: other-regions %s amount and currency must be provided together", line, currency)
		}
		micros, err := decimalToMicros(amount)
		if err != nil {
			return fmt.Errorf("store-state CSV line %d: other-regions %s amount: %w", line, currency, err)
		}
		other[currency+"_price"] = map[string]any{"amount_micros": micros, "currency": code}
	}
	if len(other) > 0 {
		basePlan["other_regions_config"] = other
	}
	return nil
}

func decimalToMicros(value string) (int64, error) {
	negative := strings.HasPrefix(value, "-")
	value = strings.TrimPrefix(value, "+")
	value = strings.TrimPrefix(value, "-")
	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" || (len(parts) == 2 && len(parts[1]) > 6) {
		return 0, fmt.Errorf("%q must be a decimal with at most 6 fractional digits", value)
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a decimal", value)
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
	}
	for len(fraction) < 6 {
		fraction += "0"
	}
	frac := int64(0)
	if fraction != "" {
		frac, err = strconv.ParseInt(fraction, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("%q is not a decimal", value)
		}
	}
	micros := whole*1_000_000 + frac
	if negative {
		micros = -micros
	}
	return micros, nil
}

func parseCSVBool(value string) (bool, error) {
	parsed, err := strconv.ParseBool(strings.ToLower(value))
	if err != nil {
		return false, fmt.Errorf("%q must be true or false", value)
	}
	return parsed, nil
}

func childMap(parent map[string]any, key string) map[string]any {
	if child, ok := parent[key].(map[string]any); ok {
		return child
	}
	child := map[string]any{}
	parent[key] = child
	return child
}

func mergeCSVScalar(target *string, value, field string, line int) error {
	if value == "" {
		return nil
	}
	if *target != "" && *target != value {
		return fmt.Errorf("store-state CSV line %d: conflicting %s values %q and %q", line, field, *target, value)
	}
	*target = value
	return nil
}

func mergeCSVMapValue(target map[string]any, key string, value any, field string, line int) error {
	if existing, ok := target[key]; ok && !reflect.DeepEqual(existing, value) {
		return fmt.Errorf("store-state CSV line %d: conflicting %s values", line, field)
	}
	target[key] = value
	return nil
}

func emptyCSVRow(row []string) bool {
	for _, value := range row {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}
