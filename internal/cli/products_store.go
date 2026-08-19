package cli

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/output"
)

func newProductsStoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "store",
		Short: "Plan and sync Product state with the App Store or Google Play",
		Long: `Product store-state plans are persisted by RevenueCat, not by the rc
process. Humans can use sync for an in-memory plan/review/apply flow. Agents can
create a plan, inspect its plan ID in a later process, then apply or discard
that exact plan. Files are optional; pass --file - to read CSV or JSON stdin.`,
	}
	cmd.AddCommand(
		newProductsStoreSyncCmd(),
		newProductsStorePlanCmd(),
		newProductsStoreShowCmd(),
		newProductsStoreApplyCmd(),
		newProductsStoreDiscardCmd(),
		newProductsStoreScreenshotCmd(),
		newProductsStoreListCmd(),
	)
	return cmd
}

func newProductsStoreListCmd() *cobra.Command {
	var status string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Product store-state plans",
		Long: `Lists the project's persisted store-state plans so a lost plan ID can be
recovered in a later session. Filter with --status (draft, planned, applied,
apply_errored, discarded, ...).`,
		Example: `  rc products store list
  rc products store list --status planned --json --no-input`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			page, err := client.StoreStatePlans.List(cmd.Context(), projectID, status)
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(page.Items))
			for _, plan := range page.Items {
				changes := ""
				if plan.Summary != nil {
					changes = fmt.Sprintf("%d create, %d modify", plan.Summary.ProductsAdded, plan.Summary.ProductsModified)
				}
				rows = append(rows, []string{plan.ID, plan.Status, changes})
			}
			return rt.Out.RenderTable(output.Table{
				Columns: []string{"PLAN ID", "STATUS", "CHANGES"},
				Rows:    rows,
				Raw:     page,
			})
		},
	}
	cmd.Flags().StringVar(&status, "status", "", "filter by plan status")
	return cmd
}

func newProductsStoreSyncCmd() *cobra.Command {
	var input storeStateInputOptions
	var planOnly bool
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "sync [app-id]",
		Short: "Gather desired state, review its plan, and optionally apply it",
		Long: `Runs the complete human workflow in one rc process: gather desired state
interactively or from CSV/JSON, persist it as a RevenueCat plan, wait for its
diff, display warnings, and ask before applying the exact plan to the App Store
or Google Play.

No local file is required. Without --file, interactive terminals prompt for
product fields and keep the answers only in process memory. For automation,
use the explicit plan/show/apply commands so separate processes share the
server-persisted plan ID. --plan-only remains as a compatibility shortcut for
creating and reviewing without applying.

Warnings about review notes, the review screenshot, and subscription group
localizations are App Review submission requirements, not creation blockers —
each warning prints a hint naming the exact CSV column or JSON field that
clears it.

For App Store subscriptions, --equalize-base-territory <TERRITORY> fills missing
territory prices from a base territory (e.g. US) during apply. After applying,
each product's live readiness is reported (see rc products store apply --help).`,
		Example: `  rc products store sync app_abc
  rc products store sync app_abc --file catalog.csv
  rc products store sync app_abc --file catalog.csv --equalize-base-territory US
  cat desired-states.json | rc products store sync app_abc --file - --input-format json --plan-only --json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, app, client, err := resolveStoreStateApp(cmd, args)
			if err != nil {
				return err
			}
			states, err := input.desiredStates(rt, app, cmd.InOrStdin())
			if err != nil {
				return err
			}
			plan, err := createAndWaitForStoreStatePlan(cmd.Context(), rt, client, projectID, states, timeout)
			if err != nil {
				return err
			}
			printStoreStatePlanPreview(rt, plan)
			if err := validatePlannedStoreState(plan); err != nil {
				return err
			}
			if planOnly {
				return renderStoreStatePlanResult(rt, plan)
			}
			return applyStoreStatePlan(cmd.Context(), rt, client, projectID, plan, timeout)
		},
	}
	input.addFlags(cmd)
	cmd.Flags().BoolVar(&planOnly, "plan-only", false, "create and review the persisted plan without applying it")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "maximum time to wait for planning and apply")
	return cmd
}

func newProductsStorePlanCmd() *cobra.Command {
	var input storeStateInputOptions
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "plan [app-id]",
		Short: "Create and review a persisted Product store-state plan",
		Long: `Creates a server-side RevenueCat plan containing the complete desired
state and computed diff. The returned plan ID remains usable after this rc
process exits. Apply that exact reviewed plan with:

  rc products store apply <plan-id> --yes

For agents, pass --json --no-input and either --file <path> or --file - with
--input-format csv|json. Do not rerun plan before applying: use the returned
plan ID so the applied state is exactly what was reviewed.

App Store submission readiness: warnings about review notes, the review
screenshot, and subscription group localizations mark App Review requirements,
not creation blockers — products create fine without them. Provide review
notes via the app_store_review_notes CSV column (or
store_state.review_information.notes in JSON); group display names via the
app_store_subscription_group_localized_name CSV column. A placeholder review
screenshot is uploaded automatically; replace it before submitting to Apple.

For App Store subscriptions, --equalize-base-territory <TERRITORY> (e.g. US)
fills missing territory prices from a base territory during apply.`,
		Example: `  rc products store plan app_abc --file catalog.csv --json --no-input
  cat desired-states.json | rc products store plan app_abc --file - --input-format json --json --no-input`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, app, client, err := resolveStoreStateApp(cmd, args)
			if err != nil {
				return err
			}
			states, err := input.desiredStates(rt, app, cmd.InOrStdin())
			if err != nil {
				return err
			}
			plan, err := createAndWaitForStoreStatePlan(cmd.Context(), rt, client, projectID, states, timeout)
			if err != nil {
				return err
			}
			printStoreStatePlanPreview(rt, plan)
			if err := validatePlannedStoreState(plan); err != nil {
				return err
			}
			return renderStoreStatePlanResult(rt, plan)
		},
	}
	input.addFlags(cmd)
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "maximum time to wait for planning")
	return cmd
}

func newProductsStoreShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "show <plan-id>",
		Short:   "Show a persisted Product store-state plan",
		Long:    `Fetches a persisted plan by ID and prints its diff, warnings, and per-Product apply status. Read-only; makes no changes.`,
		Example: `  rc products store show plan_123 --json --no-input`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			plan, err := client.StoreStatePlans.Get(cmd.Context(), projectID, args[0])
			if err != nil {
				return err
			}
			printStoreStatePlanPreview(rt, plan)
			return renderStoreStatePlanResult(rt, plan)
		},
	}
}

func newProductsStoreApplyCmd() *cobra.Command {
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "apply <plan-id>",
		Short: "Apply an already-reviewed Product store-state plan",
		Long: `Fetches the persisted plan, displays its exact diff and warnings, and
applies it to the App Store or Google Play after confirmation. Automation must
pass --yes. This command never reconstructs desired state from a local file; it
applies the plan ID supplied.

Reversibility: writes to the connected store — undo by planning and applying
the reverse state.

After applying, it reads each product's live store state and reports readiness,
because a plan the store accepted is not the same as a sellable product:
  READY       configured and sellable
  IN_PROGRESS the store is still processing (e.g. in review)
  INCOMPLETE  something is missing (unpriced territories, missing metadata) —
              each product prints the next action to take
  FAILED      the product could not be applied or isn't in the store
  UNKNOWN     the live state could not be read (the apply still succeeded)
A non-READY result is reported as a warning, never a plain success. In --json the
readiness object carries per-product verdict, raw_store_status, unpriced
territories, store warnings, and next actions.

Confirmation: prompts under TTY; pass --yes to skip. Required under --no-input.`,
		Example: `  rc products store apply plan_123 --yes --json --no-input`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			plan, err := client.StoreStatePlans.Get(cmd.Context(), projectID, args[0])
			if err != nil {
				return err
			}
			printStoreStatePlanPreview(rt, plan)
			if err := validatePlannedStoreState(plan); err != nil {
				return err
			}
			return applyStoreStatePlan(cmd.Context(), rt, client, projectID, plan, timeout)
		},
	}
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "maximum time to wait for apply")
	return cmd
}

func newProductsStoreDiscardCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "discard <plan-id>",
		Short: "Discard a Product store-state plan without applying it",
		Long: `Discards a persisted plan so it can no longer be applied. Nothing is
written to the App Store or Google Play.

Reversibility: irreversible — re-plan to review the changes again.

Confirmation: prompts under TTY; pass --yes to skip. Required under --no-input.`,
		Example: `  rc products store discard plan_123 --yes --json --no-input`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			if err := confirmOrAbort(rt, fmt.Sprintf("Discard store-state plan %s without applying it?", args[0])); err != nil {
				return err
			}
			result, err := client.StoreStatePlans.Discard(cmd.Context(), projectID, args[0])
			if err != nil {
				return err
			}
			rt.Out.Success("Discarded product store-state plan " + args[0])
			return rt.Out.Render(result)
		},
	}
}

func resolveStoreStateApp(cmd *cobra.Command, args []string) (string, *api.App, *api.Client, error) {
	rt := RuntimeFrom(cmd.Context())
	projectID, err := requireProject(rt)
	if err != nil {
		return "", nil, nil, err
	}
	client, err := rt.API()
	if err != nil {
		return "", nil, nil, err
	}
	appID, err := requireID(rt, argAt(args, 0), "app", func() ([]PickerItem, error) {
		page, err := client.Apps.List(cmd.Context(), projectID)
		if err != nil {
			return nil, err
		}
		items := make([]PickerItem, len(page.Items))
		for i, app := range page.Items {
			items[i] = PickerItem{ID: app.ID, Label: app.Name}
		}
		return items, nil
	})
	if err != nil {
		return "", nil, nil, err
	}
	app, err := client.Apps.Get(cmd.Context(), projectID, appID)
	if err != nil {
		return "", nil, nil, err
	}
	if app.Type != "app_store" && app.Type != "play_store" {
		return "", nil, nil, fmt.Errorf("app %s does not use the App Store or Play Store", appID)
	}
	return projectID, app, client, nil
}

func createAndWaitForStoreStatePlan(ctx context.Context, rt *Runtime, client *api.Client, projectID string, states []api.StoreStatePlanDesiredState, timeout time.Duration) (*api.StoreStatePlan, error) {
	rt.Out.Info(fmt.Sprintf("Loaded %d product desired state(s)", len(states)))
	created, err := client.StoreStatePlans.Create(ctx, projectID, api.StoreStatePlanCreate{DesiredStates: states})
	if err != nil {
		return nil, err
	}
	rt.Out.Success("Plan " + created.ID + " saved for review")
	if _, err := client.StoreStatePlans.Plan(ctx, projectID, created.ID); err != nil {
		return nil, err
	}
	return waitForStoreStatePlan(ctx, client.StoreStatePlans, projectID, created.ID, timeout, planningFinished)
}

func validatePlannedStoreState(plan *api.StoreStatePlan) error {
	if plan.Status == "plan_errored" {
		return fmt.Errorf("store-state plan %s failed: %s", plan.ID, optionalString(plan.ErrorMessage, "inspect plan items for details"))
	}
	return nil
}

func applyStoreStatePlan(ctx context.Context, rt *Runtime, client *api.Client, projectID string, plan *api.StoreStatePlan, timeout time.Duration) error {
	if hasStoreStateBlocker(plan) {
		return fmt.Errorf("store-state plan %s has blocker warnings and cannot be applied", plan.ID)
	}
	if !slices.Contains(plan.Actions, "apply") {
		if plan.Status == "planned_and_finished" || (plan.HasChanges != nil && !*plan.HasChanges) {
			rt.Out.Success("Store state already matches the desired state; nothing to apply")
			return renderStoreStatePlanResult(rt, plan)
		}
		return fmt.Errorf("store-state plan %s cannot be applied from status %s; available actions: %s", plan.ID, plan.Status, strings.Join(plan.Actions, ", "))
	}
	if err := confirmOrAbort(rt, fmt.Sprintf("Apply plan %s to the connected stores?", plan.ID),
		fmt.Sprintf("plan %s remains persisted and unapplied", plan.ID)); err != nil {
		return err
	}
	if _, err := client.StoreStatePlans.Apply(ctx, projectID, plan.ID); err != nil {
		return err
	}
	plan, err := waitForStoreStatePlan(ctx, client.StoreStatePlans, projectID, plan.ID, timeout, applyingFinished)
	if err != nil {
		return err
	}
	if plan.Status == "apply_errored" {
		return fmt.Errorf("store-state plan %s failed while applying: %s", plan.ID, storeStateApplyErrors(plan))
	}
	rt.Out.Info("Applied plan " + plan.ID + " to the stores; verifying readiness")
	report := verifyStoreStateReadiness(ctx, rt, client.StoreState, projectID, plan)
	return renderStoreStateApplyResult(rt, plan, report)
}

type storeStatePlansAPI interface {
	Get(context.Context, string, string) (*api.StoreStatePlan, error)
}

func waitForStoreStatePlan(ctx context.Context, service storeStatePlansAPI, projectID, planID string, timeout time.Duration, finished func(string) bool) (*api.StoreStatePlan, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		plan, err := service.Get(ctx, projectID, planID)
		if err != nil {
			return nil, err
		}
		if finished(plan.Status) {
			return plan, nil
		}
		if terminalStoreStateStatus(plan.Status) {
			return nil, fmt.Errorf("store-state plan %s reached terminal status %s", planID, plan.Status)
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timed out waiting for store-state plan %s: %w", planID, ctx.Err())
		case <-time.After(time.Second):
		}
	}
}

func planningFinished(status string) bool {
	return status == "planned" || status == "planned_and_finished" || status == "plan_errored"
}

func applyingFinished(status string) bool {
	return status == "applied" || status == "apply_errored"
}

func terminalStoreStateStatus(status string) bool {
	return status == "discarded" || status == "cancelled" || status == "expired"
}

func printStoreStatePlanPreview(rt *Runtime, plan *api.StoreStatePlan) {
	if plan.Summary != nil {
		rt.Out.Info(fmt.Sprintf("Plan %s: %d create, %d modify, %d unchanged", plan.ID, plan.Summary.ProductsAdded, plan.Summary.ProductsModified, plan.Summary.ProductsUnchanged))
	}
	for _, item := range plan.PlanItems {
		identifier := optionalString(item.StoreIdentifier, optionalString(item.ProductID, "unknown product"))
		rt.Out.Info(fmt.Sprintf("%s %s", strings.ToUpper(item.Action), identifier))
		for _, diff := range item.Diff {
			rt.Out.Info(fmt.Sprintf("  %s: %v -> %v", diff.Field, diff.FromValue, diff.ToValue))
		}
		printStoreStateWarnings(rt, item.Warnings)
	}
	printStoreStateWarnings(rt, plan.Warnings)
}

// submissionWarningHints explains warnings that mark App Store *submission*
// requirements (not creation blockers) and names the exact input that clears
// each one — otherwise the plan warns about fields it never tells you how to
// set.
var submissionWarningHints = map[string]string{
	"store_state.review_information.notes":         "needed for App Review — set app_store_review_notes in CSV or store_state.review_information.notes in JSON",
	"store_state.review_information.screenshot":    "a placeholder is uploaded so creation proceeds — attach a real one with: rc products store screenshot <product-id> --file paywall.png",
	"store_state.subscription_group_localizations": "needed for App Store submission — set app_store_subscription_group_localized_name (+ locale) in CSV or store_state.subscription_group_localizations.<locale>.name in JSON",
}

func printStoreStateWarnings(rt *Runtime, warnings []api.StoreStatePlanWarning) {
	for _, warning := range warnings {
		rt.Out.Warn(fmt.Sprintf("%s [%s]: %s", warning.Severity, warning.Field, warning.Message))
		if hint, ok := submissionWarningHints[warning.Field]; ok {
			rt.Out.Hint(hint)
		}
	}
}

func renderStoreStatePlanResult(rt *Runtime, plan *api.StoreStatePlan) error {
	rows := make([][]string, 0, len(plan.PlanItems))
	for _, item := range plan.PlanItems {
		rows = append(rows, []string{
			optionalString(item.StoreIdentifier, optionalString(item.ProductID, "")),
			item.Action,
			fmt.Sprintf("%d", len(item.Diff)),
			fmt.Sprintf("%d", len(item.Warnings)),
			optionalString(item.ApplyStatus, ""),
		})
	}
	return rt.Out.RenderTable(output.Table{
		Columns: []string{"PRODUCT", "ACTION", "CHANGES", "WARNINGS", "APPLY STATUS"},
		Rows:    rows,
		Raw:     plan,
	})
}

func hasStoreStateBlocker(plan *api.StoreStatePlan) bool {
	for _, warning := range plan.Warnings {
		if warning.Severity == "blocker" {
			return true
		}
	}
	for _, item := range plan.PlanItems {
		for _, warning := range item.Warnings {
			if warning.Severity == "blocker" {
				return true
			}
		}
	}
	return false
}

func storeStateApplyErrors(plan *api.StoreStatePlan) string {
	messages := make([]string, 0)
	for _, item := range plan.PlanItems {
		if item.ApplyErrorMessage != nil {
			messages = append(messages, *item.ApplyErrorMessage)
		}
	}
	if len(messages) == 0 {
		return optionalString(plan.ErrorMessage, "inspect plan items for details")
	}
	return strings.Join(messages, "; ")
}

func optionalString(value *string, fallback string) string {
	if value == nil || *value == "" {
		return fallback
	}
	return *value
}
