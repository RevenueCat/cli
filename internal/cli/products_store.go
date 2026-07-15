package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/output"
	"github.com/revenuecat/cli/internal/tui"
)

func newProductsStoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "store",
		Short: "Plan and sync product configuration with an app store",
	}
	cmd.AddCommand(newProductsStoreSyncCmd())
	return cmd
}

func newProductsStoreSyncCmd() *cobra.Command {
	var file string
	var planOnly bool
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "sync [app-id]",
		Short: "Plan, review, and apply a canonical store-state CSV",
		Long: `Reads Khepri's canonical product store-state CSV locally and sends its
desired product states to RevenueCat. RevenueCat compares them with the live
App Store or Play Store state before anything is changed.

The planned product diffs and warnings are always reviewed before apply.
Interactive runs prompt for confirmation; non-interactive runs require --yes.
Use --plan-only to create and inspect the backend plan without applying it.

This POC uses development-only RevenueCat v2 endpoints and currently requires
the PRODUCT_CATALOG_PRODUCT_PRICE_MANAGER feature flag in Khepri.`,
		Example: `  rc products store sync app_abc --file catalog.csv --plan-only
  rc products store sync app_abc --file catalog.csv
  rc products store sync app_abc --file catalog.csv --yes --json`,
		Args: cobra.MaximumNArgs(1),
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
				return err
			}
			file = valueOrEnv(file, "RC_STORE_STATE_FILE")
			if file == "" {
				if rt.Globals.NoInput || !tui.IsInteractive() {
					return errors.New("--file is required")
				}
				if err := tui.Form(false).Field(
					huh.NewInput().Title("Canonical store-state CSV path").Value(&file).Validate(tui.Required("CSV path")),
				).Run(); err != nil {
					return err
				}
			}
			desiredStates, err := readStoreStateCSV(file, appID)
			if err != nil {
				return err
			}
			rt.Out.Info(fmt.Sprintf("Loaded %d product desired state(s) from %s", len(desiredStates), file))
			created, err := client.StoreStatePlans.Create(cmd.Context(), projectID, api.StoreStatePlanCreate{DesiredStates: desiredStates})
			if err != nil {
				return err
			}
			rt.Out.Info("Created store-state plan " + created.ID)
			if _, err := client.StoreStatePlans.Plan(cmd.Context(), projectID, created.ID); err != nil {
				return err
			}
			plan, err := waitForStoreStatePlan(cmd.Context(), client.StoreStatePlans, projectID, created.ID, timeout, planningFinished)
			if err != nil {
				return err
			}
			printStoreStatePlanPreview(rt, plan)
			if plan.Status == "plan_errored" {
				return fmt.Errorf("store-state plan %s failed: %s", plan.ID, optionalString(plan.ErrorMessage, "inspect plan items for details"))
			}
			if planOnly {
				return renderStoreStatePlanResult(rt, plan)
			}
			if hasStoreStateBlocker(plan) {
				return fmt.Errorf("store-state plan %s has blocker warnings and cannot be applied", plan.ID)
			}
			if !slices.Contains(plan.Actions, "apply") {
				rt.Out.Success("Store state already matches the CSV; nothing to apply")
				return renderStoreStatePlanResult(rt, plan)
			}
			if !rt.Globals.AssumeYes {
				confirmed, err := tui.Confirm(rt.Globals.NoInput, fmt.Sprintf("Apply plan %s to the app store?", plan.ID))
				if err != nil {
					return err
				}
				if !confirmed {
					return errors.New("cancelled; the RevenueCat plan was left unapplied")
				}
			}
			if _, err := client.StoreStatePlans.Apply(cmd.Context(), projectID, plan.ID); err != nil {
				return err
			}
			plan, err = waitForStoreStatePlan(cmd.Context(), client.StoreStatePlans, projectID, plan.ID, timeout, applyingFinished)
			if err != nil {
				return err
			}
			if plan.Status == "apply_errored" {
				return fmt.Errorf("store-state plan %s failed while applying: %s", plan.ID, storeStateApplyErrors(plan))
			}
			rt.Out.Success("Applied product store state plan " + plan.ID)
			return renderStoreStatePlanResult(rt, plan)
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "canonical store-state CSV path (env: RC_STORE_STATE_FILE)")
	cmd.Flags().BoolVar(&planOnly, "plan-only", false, "create and review the plan without applying it")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "maximum time to wait for planning and apply")
	return cmd
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
		for _, warning := range item.Warnings {
			rt.Out.Warn(fmt.Sprintf("%s [%s]: %s", warning.Severity, warning.Field, warning.Message))
		}
	}
	for _, warning := range plan.Warnings {
		rt.Out.Warn(fmt.Sprintf("%s [%s]: %s", warning.Severity, warning.Field, warning.Message))
	}
}

func renderStoreStatePlanResult(rt *Runtime, plan *api.StoreStatePlan) error {
	rows := make([][]string, 0, len(plan.PlanItems))
	for _, item := range plan.PlanItems {
		rows = append(rows, []string{
			optionalString(item.StoreIdentifier, optionalString(item.ProductID, "")),
			item.Action,
			strconvLen(item.Diff),
			strconvLen(item.Warnings),
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
	errors := make([]string, 0)
	for _, item := range plan.PlanItems {
		if item.ApplyErrorMessage != nil {
			errors = append(errors, *item.ApplyErrorMessage)
		}
	}
	if len(errors) == 0 {
		return optionalString(plan.ErrorMessage, "inspect plan items for details")
	}
	return strings.Join(errors, "; ")
}

func optionalString(value *string, fallback string) string {
	if value == nil || *value == "" {
		return fallback
	}
	return *value
}

func strconvLen[T any](values []T) string {
	return fmt.Sprintf("%d", len(values))
}
