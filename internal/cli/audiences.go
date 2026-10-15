package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/output"
	"github.com/revenuecat/cli/internal/tui"
)

func newAudiencesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "audiences",
		Aliases: []string{"audience"},
		Short:   "View Audiences (saved customer segments)",
		Long: `An Audience is a saved customer segment defined by membership rules, such as
country, platform, or custom attributes. Targeting rules and experiments use
Audiences to decide who they apply to.`,
		Example: `  rc audiences list
  rc audiences show aud_abc --stats --usage`,
	}
	cmd.AddCommand(
		newAudiencesListCmd(),
		newAudiencesShowCmd(),
	)
	return cmd
}

func newAudiencesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List Audiences",
		Long:  `Lists the project's Audiences with their membership rules.`,
		Example: `  rc audiences list
  rc audiences list --json | jq '.data.items[] | {id, name}'`,
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
			page, err := client.Audiences.List(cmd.Context(), projectID)
			if err != nil {
				return err
			}

			if rt.CanPrompt() {
				items := make([]tui.BrowserItem, len(page.Items))
				for i, a := range page.Items {
					items[i] = audienceToItem(projectID, a)
				}
				return tui.RunBrowserTable("Audiences", []string{"ID", "NAME", "RULES"}, items)
			}

			rows := make([][]string, 0, len(page.Items))
			for _, a := range page.Items {
				rows = append(rows, []string{a.ID, a.Name, audienceRulesSummary(a.Rules), formatMillis(int64(a.CreatedAt))})
			}
			return rt.Out.RenderTable(output.Table{
				Columns: []string{"ID", "NAME", "RULES", "CREATED"},
				Rows:    rows,
				Raw:     page,
			})
		},
	}
}

func newAudiencesShowCmd() *cobra.Command {
	var stats, sample, usage bool
	cmd := &cobra.Command{
		Use:   "show [id]",
		Short: "Show an Audience",
		Long: `Shows an Audience's membership rules. Add --stats for its size and revenue,
--sample for a sample of matching customers, and --usage for the targeting
rules and experiments that use it. Omit the ID in a terminal to pick from a list.`,
		Example: `  rc audiences show aud_abc
  rc audiences show aud_abc --stats --sample
  rc audiences show aud_abc --usage --json`,
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
			audienceID, err := requireID(rt, argAt(args, 0), "audience", func() ([]PickerItem, error) {
				return audiencePickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}

			var expand []api.AudienceExpand
			if stats {
				expand = append(expand, api.AudienceExpandStats)
			}
			if sample {
				expand = append(expand, api.AudienceExpandCustomerSample)
			}
			if usage {
				expand = append(expand, api.AudienceExpandUsedBy)
			}
			a, err := client.Audiences.Get(cmd.Context(), projectID, audienceID, expand...)
			if err != nil {
				return err
			}
			return rt.Out.RenderCard(audienceCard(projectID, a))
		},
	}
	cmd.Flags().BoolVar(&stats, "stats", false, "include size and revenue stats")
	cmd.Flags().BoolVar(&sample, "sample", false, "include a sample of matching customers")
	cmd.Flags().BoolVar(&usage, "usage", false, "include the targeting rules and experiments that use the Audience")
	return cmd
}

func audienceCard(projectID string, a *api.Audience) output.Card {
	card := output.Card{
		Title:    a.Name,
		Subtitle: fmt.Sprintf("%s · created %s", a.ID, nonEmpty(formatMillis(int64(a.CreatedAt)), "—")),
		Raw:      a,
	}

	rules := output.CardSection{Heading: "Rules", Empty: "matches every customer"}
	for i, group := range a.Rules.Groups {
		key := "match"
		if i > 0 {
			key = "or"
		}
		rules.Lines = append(rules.Lines, output.CardLine{Key: key, Value: audienceGroupSummary(group)})
	}
	card.Sections = append(card.Sections, rules)

	if a.Stats != nil {
		s := a.Stats
		heading := "Stats"
		if s.IsApproximate {
			heading = "Stats (approximate)"
		}
		card.Sections = append(card.Sections, output.CardSection{Heading: heading, Lines: []output.CardLine{
			{Key: "customers", Value: fmt.Sprint(s.TotalCustomers)},
			{Key: "active subscriptions", Value: fmt.Sprint(s.ActiveSubscriptions)},
			{Key: "active trials", Value: fmt.Sprint(s.ActiveTrials)},
			{Key: "revenue", Value: fmt.Sprintf("%.2f %s", s.TotalRevenue, s.Currency)},
		}})
	}

	if a.CustomerSample != nil {
		section := output.CardSection{Heading: "Sample customers", Empty: "no matching customers"}
		if len(a.CustomerSample) > 0 {
			tab := &output.CardTable{Columns: []string{"APP USER ID", "STATUS", "TOTAL SPENT", "LAST SEEN"}}
			for _, c := range a.CustomerSample {
				spent := "—"
				if c.TotalSpent != nil {
					spent = fmt.Sprintf("%.2f %s", *c.TotalSpent, c.Currency)
				}
				tab.Rows = append(tab.Rows, []string{c.AppUserID, c.Status, spent, formatMillisPtr(c.LastSeenAt)})
			}
			section.Table = tab
		}
		card.Sections = append(card.Sections, section)
	}

	if a.UsedBy != nil {
		section := output.CardSection{Heading: "Used by", Empty: "not used by any targeting rule or experiment"}
		if len(a.UsedBy.TargetingRules)+len(a.UsedBy.Experiments) > 0 {
			tab := &output.CardTable{Columns: []string{"TYPE", "ID", "NAME", "STATE"}}
			for _, r := range a.UsedBy.TargetingRules {
				tab.Rows = append(tab.Rows, []string{"Targeting rule", r.ID, r.DisplayName, r.State})
			}
			for _, e := range a.UsedBy.Experiments {
				tab.Rows = append(tab.Rows, []string{"Experiment", e.ID, e.DisplayName, e.Status})
			}
			section.Table = tab
		}
		card.Sections = append(card.Sections, section)
	}

	card.Sections = append(card.Sections, output.CardSection{Heading: "Dashboard", Lines: []output.CardLine{
		{Key: "url", Value: audienceDashboardURL(projectID, *a)},
	}})
	return card
}

// audienceRulesSummary is the one-line form for tables: groups are OR'd,
// conditions within a group are AND'd.
func audienceRulesSummary(rules api.AudienceRules) string {
	if len(rules.Groups) == 0 {
		return "everyone"
	}
	groups := make([]string, len(rules.Groups))
	for i, g := range rules.Groups {
		groups[i] = audienceGroupSummary(g)
	}
	if len(groups) == 1 {
		return groups[0]
	}
	return "(" + strings.Join(groups, ") or (") + ")"
}

func audienceGroupSummary(group api.AudienceRuleGroup) string {
	conditions := make([]string, len(group.Conditions))
	for i, c := range group.Conditions {
		parts := []string{c.Field, c.Operator}
		if c.Value != "" {
			parts = append(parts, c.Value)
		}
		if c.Currency != "" {
			parts = append(parts, c.Currency)
		}
		conditions[i] = strings.Join(parts, " ")
	}
	return strings.Join(conditions, " and ")
}

func audienceDashboardURL(projectID string, a api.Audience) string {
	if a.CustomerListID == "" {
		return dashboardURL(projectID, "customer-lists")
	}
	return dashboardURL(projectID, "customer-lists", a.CustomerListID)
}

func audienceToItem(projectID string, a api.Audience) tui.BrowserItem {
	rules := audienceRulesSummary(a.Rules)
	return tui.BrowserItem{
		ID:     a.ID,
		Label:  a.Name,
		Meta:   rules,
		Row:    []string{a.ID, a.Name, rules},
		WebURL: audienceDashboardURL(projectID, a),
		Fields: []tui.BrowserField{
			{Key: "ID", Value: a.ID},
			{Key: "Name", Value: a.Name},
			{Key: "Rules", Value: rules},
			{Key: "Created", Value: formatMillis(int64(a.CreatedAt))},
		},
	}
}

func audiencePickerItems(ctx context.Context, client *api.Client, projectID string) ([]PickerItem, error) {
	page, err := client.Audiences.List(ctx, projectID)
	if err != nil {
		return nil, err
	}
	items := make([]PickerItem, len(page.Items))
	for i, a := range page.Items {
		items[i] = PickerItem{ID: a.ID, Label: a.Name}
	}
	return items, nil
}
