package cli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/tui"
)

func newPaywallsCreateCmd() *cobra.Command {
	var offeringID, name string
	var standalone, duplicateOffering bool
	var automaticallyScaleFontSize bool
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a draft paywall",
		Long: `Creates a draft paywall.

Attached to an offering, it starts from RevenueCat's default template. An
offering can only have one paywall; if the one you pick already has one, the
command offers to duplicate the offering (same packages and products) and
attach the new paywall to the copy.

Standalone (--standalone, or pick "no offering" interactively) creates a
blank draft with no offering. Design it with rc paywalls edit and attach it
later in the dashboard.`,
		Example: `  rc paywalls create --offering-id ofrng_default
  rc paywalls create --standalone --name "Summer sale"
  rc paywalls create --offering-id ofrng_default --json --no-input`,
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

			interactive := offeringID == "" && !standalone && !rt.Globals.NoInput && tui.IsInteractive()
			if offeringID == "" && !standalone && !interactive {
				standalone = true // non-interactive with no offering: standalone
			}

			if interactive {
				// Show what exists before asking anything: which offerings
				// have paywalls, which are free to attach to, and any
				// standalone drafts already lying around.
				offerings, paywalls, err := paywallCreateState(cmd.Context(), client, projectID)
				if err != nil {
					return err
				}
				choice, pickedOffering, err := promptPaywallCreate(rt, offerings, paywalls)
				if err != nil {
					return err
				}
				switch choice {
				case createStandalone:
					standalone = true
				case createForOffering:
					offeringID = pickedOffering
				case createDuplicate:
					// The menu already chose to duplicate this offering; go
					// straight to the copy (prompts only for the new name).
					// Don't re-ask the duplicate/standalone question.
					offeringID, standalone, err = duplicateOfferingForPaywall(cmd, rt, client, projectID, pickedOffering, "", "")
					if err != nil {
						return err
					}
				}
			}

			// Non-interactive / flag path: still guard the one-per-offering rule.
			if !standalone && offeringID != "" {
				existing, err := paywallForOffering(cmd.Context(), client, projectID, offeringID)
				if err != nil {
					return err
				}
				if existing != nil {
					offeringID, standalone, err = resolvePaywallConflict(cmd, rt, client, projectID, offeringID, existing, duplicateOffering)
					if err != nil {
						return err
					}
				}
			}

			var paywall *api.Paywall
			if standalone {
				if name == "" && !rt.Globals.NoInput && tui.IsInteractive() {
					name = "Untitled Paywall"
					if err := tui.Form(false).
						Field(huh.NewInput().Title("Paywall name").Value(&name).Validate(tui.Required("name"))).
						Run(); err != nil {
						return err
					}
				}
				paywall, err = client.Paywalls.CreateFromComponents(cmd.Context(), projectID, api.PaywallComponentsCreate{
					Name:                    name,
					ComponentsConfig:        json.RawMessage(minimalComponentsConfig),
					ComponentsLocalizations: json.RawMessage(`{"en_US": {}}`),
					DefaultLocale:           "en_US",
				})
			} else {
				paywall, err = client.Paywalls.Create(cmd.Context(), projectID, api.PaywallCreate{
					OfferingID:                 offeringID,
					AutomaticallyScaleFontSize: automaticallyScaleFontSize,
				})
			}
			if err != nil {
				return err
			}

			rt.Out.Success("Created draft paywall " + paywall.ID)
			if !rt.Out.IsJSON() {
				rt.Out.Blank()
				rt.Out.Field("View it", paywallBuilderURL(projectID, paywall.ID))
				rt.Out.Field("Design it", "rc paywalls edit "+paywall.ID)
				if standalone {
					rt.Out.Field("Attach it", "dashboard → Paywalls → "+paywall.ID+" → attach to an offering")
				} else {
					rt.Out.Field("Publish when ready", "rc paywalls publish "+paywall.ID)
				}
				return nil
			}
			return rt.Out.Render(paywall)
		},
	}
	cmd.Flags().StringVar(&offeringID, "offering-id", "", "offering to attach (picker shown in TTY if omitted)")
	cmd.Flags().StringVar(&name, "name", "", "paywall name (standalone paywalls)")
	cmd.Flags().BoolVar(&standalone, "standalone", false, "create without an offering; attach later")
	cmd.Flags().BoolVar(&duplicateOffering, "duplicate-offering", false, "if the chosen offering already has a paywall, fork the offering (same packages and products) and attach the new paywall there")
	cmd.Flags().BoolVar(&automaticallyScaleFontSize, "automatically-scale-font-size", true, "automatically scale paywall fonts")
	return cmd
}

const (
	createForOffering = iota
	createStandalone
	createDuplicate
)

func paywallCreateState(ctx context.Context, client *api.Client, projectID string) ([]api.Offering, []api.Paywall, error) {
	offeringsPage, err := client.Offerings.List(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	paywallsPage, err := client.Paywalls.List(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	return offeringsPage.Items, paywallsPage.Items, nil
}

func paywallOnOffering(paywalls []api.Paywall, offeringID string) *api.Paywall {
	for i := range paywalls {
		if paywalls[i].OfferingID == offeringID {
			return &paywalls[i]
		}
	}
	return nil
}

// promptPaywallCreate shows current state (offerings and their paywall status,
// standalone drafts) and returns the chosen action. This is the guided-command
// contract: state first, then the decision.
func promptPaywallCreate(rt *Runtime, offerings []api.Offering, paywalls []api.Paywall) (int, string, error) {
	rt.Out.Title("Create a paywall")
	rt.Out.Lead("An offering can have one paywall. Here's what this project already has.")

	for _, o := range offerings {
		label := o.LookupKey
		if o.IsCurrent {
			label += " (current)"
		}
		if pw := paywallOnOffering(paywalls, o.ID); pw != nil {
			rt.Out.Field(label, "has a paywall", paywallLabel(pw))
		} else {
			rt.Out.Field(label, "no paywall yet")
		}
	}
	standaloneCount := 0
	for i := range paywalls {
		if paywalls[i].OfferingID == "" {
			standaloneCount++
		}
	}
	if standaloneCount > 0 {
		rt.Out.Field("Standalone drafts", fmt.Sprintf("%d not attached to any offering", standaloneCount))
	}
	rt.Out.Blank()

	// Build the option list from real state: free offerings attach directly,
	// taken offerings offer duplication, plus standalone.
	type opt struct {
		label      string
		action     int
		offeringID string
	}
	opts := make([]opt, 0, len(offerings)+1)
	for _, o := range offerings {
		if paywallOnOffering(paywalls, o.ID) == nil {
			opts = append(opts, opt{"Attach to " + o.LookupKey, createForOffering, o.ID})
		}
	}
	for _, o := range offerings {
		if paywallOnOffering(paywalls, o.ID) != nil {
			opts = append(opts, opt{"Duplicate " + o.LookupKey, createDuplicate, o.ID})
		}
	}
	opts = append(opts, opt{"Standalone", createStandalone, ""})

	huhOpts := make([]huh.Option[int], len(opts))
	for i, o := range opts {
		huhOpts[i] = huh.NewOption(o.label, i)
	}
	selected := 0
	if err := tui.Form(false).
		Field(huh.NewSelect[int]().Title("What do you want to create?").Options(huhOpts...).Value(&selected)).
		Run(); err != nil {
		return 0, "", err
	}
	chosen := opts[selected]
	rt.Out.Answer("Create", chosen.label)
	return chosen.action, chosen.offeringID, nil
}

func paywallLabel(pw *api.Paywall) string {
	name := pw.Name
	if name == "" {
		name = pw.ID
	}
	if pw.PublishedAt != nil {
		return name + ", published"
	}
	return name + ", draft"
}

func paywallForOffering(ctx context.Context, client *api.Client, projectID, offeringID string) (*api.Paywall, error) {
	page, err := client.Paywalls.List(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for i := range page.Items {
		if page.Items[i].OfferingID == offeringID {
			return &page.Items[i], nil
		}
	}
	return nil, nil
}

// resolvePaywallConflict handles "this offering already has a paywall":
// duplicate the offering (same packages and products) and attach there, go
// standalone, or abort. Returns the offering to use and whether standalone.
func resolvePaywallConflict(cmd *cobra.Command, rt *Runtime, client *api.Client, projectID, offeringID string, existing *api.Paywall, duplicate bool) (string, bool, error) {
	if rt.Globals.NoInput || !tui.IsInteractive() {
		// Agents get a real path, not "do it interactively": --duplicate-offering
		// forks the offering; --standalone drops the offering. Without either,
		// fail with both options named so the agent doesn't hand-roll a copy.
		if duplicate {
			return duplicateOfferingForPaywall(cmd, rt, client, projectID, offeringID, "", "")
		}
		return "", false, fmt.Errorf("offering %s already has a paywall (%s); an offering can only have one. Re-run with --duplicate-offering to fork it (same packages and products) and attach there, or --standalone to create the paywall without an offering", offeringID, existing.ID)
	}
	rt.Out.Warn(fmt.Sprintf("Offering %s already has a paywall (%s) — an offering can only have one.", offeringID, existing.ID))

	const (
		choiceDuplicate = iota
		choiceStandalone
		choiceAbort
	)
	choice, err := decide(rt, "What would you like to do?", nil, []Choice[int]{
		{choiceDuplicate, "Duplicate the offering (same packages and products) and attach there", "--duplicate-offering"},
		{choiceStandalone, "Create the paywall standalone instead (attach later)", "--standalone"},
		{choiceAbort, "Cancel", ""},
	})
	if err != nil {
		return "", false, err
	}
	switch choice {
	case choiceStandalone:
		rt.Out.Answer("Paywall", "standalone")
		return "", true, nil
	case choiceAbort:
		return "", false, fmt.Errorf("aborted")
	}

	return duplicateOfferingForPaywall(cmd, rt, client, projectID, offeringID, "", "")
}

// duplicateOfferingForPaywall copies an offering (packages + products) so a
// new paywall can attach to the copy. Empty lookupKey/displayName default to
// the source plus "_copy" (the non-interactive path). Returns the new
// offering ID.
func duplicateOfferingForPaywall(cmd *cobra.Command, rt *Runtime, client *api.Client, projectID, offeringID, lookupKey, displayName string) (string, bool, error) {
	source, err := client.Offerings.Get(cmd.Context(), projectID, offeringID)
	if err != nil {
		return "", false, err
	}
	if lookupKey == "" {
		lookupKey = source.LookupKey + "_copy"
	}
	if displayName == "" {
		displayName = source.DisplayName + " Copy"
	}

	rt.Out.Info("Duplicating offering " + source.LookupKey + " → " + lookupKey + "…")
	duplicate, err := client.Offerings.Create(cmd.Context(), projectID, api.OfferingCreate{
		LookupKey:   lookupKey,
		DisplayName: displayName,
	})
	if err != nil {
		return "", false, err
	}
	packages, err := client.Packages.List(cmd.Context(), projectID, offeringID)
	if err != nil {
		return "", false, err
	}
	for _, pkg := range packages.Items {
		created, err := client.Packages.Create(cmd.Context(), projectID, duplicate.ID, api.PackageCreate{
			LookupKey:   pkg.LookupKey,
			DisplayName: pkg.DisplayName,
		})
		if err != nil {
			return "", false, fmt.Errorf("copy package %s: %w", pkg.LookupKey, err)
		}
		products, err := client.Packages.ListProducts(cmd.Context(), projectID, pkg.ID)
		if err != nil {
			return "", false, err
		}
		productIDs := make([]string, 0, len(products.Items))
		for _, assoc := range products.Items {
			productIDs = append(productIDs, assoc.Product.ID)
		}
		if len(productIDs) > 0 {
			if err := client.Packages.AttachProducts(cmd.Context(), projectID, created.ID, productIDs); err != nil {
				return "", false, fmt.Errorf("attach products to %s: %w", pkg.LookupKey, err)
			}
		}
		rt.Out.Info("  copied package " + pkg.LookupKey + " (" + fmt.Sprintf("%d product(s)", len(productIDs)) + ")")
	}
	rt.Out.Answer("Offering", displayName+"  ("+duplicate.ID+")")
	return duplicate.ID, false, nil
}
