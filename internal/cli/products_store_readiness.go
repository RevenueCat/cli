package cli

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/revenuecat/cli/internal/api"
)

type readinessVerdict string

const (
	readinessReady      readinessVerdict = "READY"
	readinessInProgress readinessVerdict = "IN_PROGRESS"
	readinessIncomplete readinessVerdict = "INCOMPLETE"
	readinessFailed     readinessVerdict = "FAILED"
	readinessUnknown    readinessVerdict = "UNKNOWN"
)

type productReadiness struct {
	ProductID           string           `json:"product_id"`
	Verdict             readinessVerdict `json:"verdict"`
	RawStoreStatus      *string          `json:"raw_store_status"`
	UnpricedTerritories []string         `json:"unpriced_territories"`
	// Warnings is the store's own remedy text (from the live read) — what to fix
	// to make the product sellable. NextActions adds CLI-specific guidance.
	Warnings    []string `json:"warnings,omitempty"`
	NextActions []string `json:"next_actions,omitempty"`
}

type readinessReport struct {
	Overall  readinessVerdict   `json:"overall"`
	Products []productReadiness `json:"products"`
}

// storeStateApplyResult inlines the applied plan and appends the live readiness
// verdict so --json consumers get both in one envelope.
type storeStateApplyResult struct {
	*api.StoreStatePlan
	Readiness *readinessReport `json:"readiness,omitempty"`
}

// readinessSeverity ranks verdicts so the overall report can take the worst.
// UNKNOWN sits below READY: a best-effort read failure must not eclipse a real
// verdict from a product we could actually inspect.
func readinessSeverity(v readinessVerdict) int {
	switch v {
	case readinessFailed:
		return 4
	case readinessIncomplete:
		return 3
	case readinessInProgress:
		return 2
	case readinessReady:
		return 1
	default:
		return 0
	}
}

func worstReadiness(products []productReadiness) readinessVerdict {
	worst := readinessReady
	for i, p := range products {
		if i == 0 {
			worst = p.Verdict
			continue
		}
		if readinessSeverity(p.Verdict) > readinessSeverity(worst) {
			worst = p.Verdict
		}
	}
	return worst
}

// classifyProductReadiness maps a live store read into a readiness verdict. It
// is pure so the mapping can be exercised without the network.
func classifyProductReadiness(productID string, state *api.LiveStoreState, applyStatus *string) productReadiness {
	pr := productReadiness{ProductID: productID}
	if applyStatus != nil && *applyStatus == "failed" {
		pr.Verdict = readinessFailed
		return pr
	}
	if state == nil {
		pr.Verdict = readinessUnknown
		return pr
	}

	pr.UnpricedTerritories = unpricedTerritories(state.Common)

	var status string
	if state.StoreStatus != nil {
		status = state.StoreStatus.Status
		pr.RawStoreStatus = state.StoreStatus.RawStoreStatus
	}
	if status == "not_found" {
		pr.Verdict = readinessFailed
		return pr
	}

	verdict, resolved := classifyRawStoreStatus(pr.RawStoreStatus, len(pr.UnpricedTerritories) == 0)
	if !resolved {
		verdict = classifyNormalizedStatus(status, len(pr.UnpricedTerritories) == 0)
	}

	// Available-but-unpriced territories force at least INCOMPLETE regardless of
	// how the store itself reports the product.
	if len(pr.UnpricedTerritories) > 0 && readinessSeverity(verdict) < readinessSeverity(readinessIncomplete) {
		verdict = readinessIncomplete
	}
	pr.Verdict = verdict
	pr.Warnings = state.Warnings
	pr.NextActions = readinessNextActions(pr)
	return pr
}

// readinessNextActions turns a non-ready verdict into concrete CLI steps, so an
// agent isn't left inferring the remedy from a raw status string.
func readinessNextActions(pr productReadiness) []string {
	if pr.Verdict == readinessReady || pr.Verdict == readinessInProgress {
		return nil
	}
	var actions []string
	if len(pr.UnpricedTerritories) > 0 {
		actions = append(actions, "set prices for the unpriced territories, or fill them from a base territory: re-run with --equalize-base-territory <TERRITORY> (e.g. US)")
	}
	if pr.RawStoreStatus != nil && strings.EqualFold(*pr.RawStoreStatus, "MISSING_METADATA") {
		actions = append(actions, "add the product's required metadata (localizations/review info) and re-apply; attach a real review screenshot with: rc products store screenshot <product-id> --file <path>")
	}
	return actions
}

// classifyRawStoreStatus resolves the store's own raw state. The second return
// is false for raw states not covered here, signalling a fall back to the
// normalized status.
func classifyRawStoreStatus(raw *string, priced bool) (readinessVerdict, bool) {
	if raw == nil {
		return "", false
	}
	switch strings.ToUpper(strings.TrimSpace(*raw)) {
	case "APPROVED":
		if priced {
			return readinessReady, true
		}
		return readinessIncomplete, true
	case "MISSING_METADATA", "READY_TO_SUBMIT", "PREPARE_FOR_SUBMISSION",
		"DEVELOPER_ACTION_NEEDED", "REJECTED", "DEVELOPER_REJECTED":
		return readinessIncomplete, true
	case "WAITING_FOR_REVIEW", "IN_REVIEW", "PENDING_BINARY_APPROVAL", "PROCESSING_CONTENT", "WAITING_FOR_UPLOAD":
		return readinessInProgress, true
	case "REMOVED_FROM_SALE", "DEVELOPER_REMOVED_FROM_SALE", "NOT_FOR_SALE":
		return readinessIncomplete, true
	default:
		return "", false
	}
}

func classifyNormalizedStatus(status string, priced bool) readinessVerdict {
	switch status {
	case "ok":
		if priced {
			return readinessReady
		}
		return readinessIncomplete
	case "not_found":
		return readinessFailed
	default:
		return readinessIncomplete
	}
}

func unpricedTerritories(common *api.StoreStateCommon) []string {
	if common == nil || common.Availability == nil {
		return nil
	}
	var priced map[string]api.TerritoryPrice
	if common.Pricing != nil {
		priced = common.Pricing.TerritoryPrices
	}
	var unpriced []string
	for territory, available := range common.Availability.Territories {
		if !available {
			continue
		}
		if _, ok := priced[territory]; !ok {
			unpriced = append(unpriced, territory)
		}
	}
	sort.Strings(unpriced)
	return unpriced
}

func isStoreStateNotFound(err error) bool {
	var ae *api.APIError
	if errors.As(err, &ae) {
		return ae.Status == 404 || ae.Type == "resource_missing"
	}
	return false
}

type storeStateReader interface {
	Get(ctx context.Context, projectID, productID string) (*api.LiveStoreState, error)
}

// verifyStoreStateReadiness reads each applied product's live state and reports
// how ready it is. It is best-effort: a read that errors for reasons other than
// not-found is recorded as UNKNOWN so a successful apply is never reported as a
// failure.
func verifyStoreStateReadiness(ctx context.Context, rt *Runtime, reader storeStateReader, projectID string, plan *api.StoreStatePlan) *readinessReport {
	report := &readinessReport{}
	for _, item := range plan.PlanItems {
		if item.ProductID == nil {
			continue
		}
		productID := *item.ProductID
		if item.ApplyStatus != nil && *item.ApplyStatus == "failed" {
			report.Products = append(report.Products, productReadiness{ProductID: productID, Verdict: readinessFailed})
			continue
		}
		state, err := reader.Get(ctx, projectID, productID)
		if err != nil {
			if isStoreStateNotFound(err) {
				report.Products = append(report.Products, productReadiness{ProductID: productID, Verdict: readinessFailed})
				continue
			}
			report.Products = append(report.Products, productReadiness{ProductID: productID, Verdict: readinessUnknown})
			continue
		}
		report.Products = append(report.Products, classifyProductReadiness(productID, state, item.ApplyStatus))
	}
	report.Overall = worstReadiness(report.Products)
	printReadinessReport(rt, report)
	return report
}

func printReadinessReport(rt *Runtime, report *readinessReport) {
	for _, p := range report.Products {
		rt.Out.Info(formatProductReadiness(p))
		for _, w := range p.Warnings {
			rt.Out.Warn("  store: " + w)
		}
		for _, a := range p.NextActions {
			rt.Out.Hint(a)
		}
	}
	msg := fmt.Sprintf("Store readiness: %s", report.Overall)
	switch report.Overall {
	case readinessReady:
		rt.Out.Success(msg)
	case readinessInProgress, readinessUnknown:
		rt.Out.Info(msg)
	default:
		rt.Out.Warn(msg)
	}
}

func formatProductReadiness(p productReadiness) string {
	parts := []string{string(p.Verdict), p.ProductID}
	if p.RawStoreStatus != nil && *p.RawStoreStatus != "" {
		parts = append(parts, "store status "+*p.RawStoreStatus)
	}
	line := strings.Join(parts, " ")
	if len(p.UnpricedTerritories) > 0 {
		line += fmt.Sprintf(" (available but unpriced: %s)", strings.Join(p.UnpricedTerritories, ", "))
	}
	return line
}

func renderStoreStateApplyResult(rt *Runtime, plan *api.StoreStatePlan, report *readinessReport) error {
	if rt.Out.IsJSON() {
		return rt.Out.Render(storeStateApplyResult{StoreStatePlan: plan, Readiness: report})
	}
	return renderStoreStatePlanResult(rt, plan)
}
