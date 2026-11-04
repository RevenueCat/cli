package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/output"
)

type paywallValidationError struct {
	issues []api.PaywallValidationIssue
}

func (e *paywallValidationError) Error() string {
	lines := []string{fmt.Sprintf("paywall not published: it has %s that would also block publishing from the dashboard:", pluralize(len(e.issues), "validation error"))}
	for _, issue := range e.issues {
		lines = append(lines, "  - "+describeValidationIssue(issue))
	}
	return strings.Join(lines, "\n")
}

type paywallValidationUnavailableError struct {
	cause error
}

func (e *paywallValidationUnavailableError) Error() string {
	return fmt.Sprintf("paywall not published: it could not be validated first (%v), and the dashboard also refuses to publish without validation", e.cause)
}

func (e *paywallValidationUnavailableError) Unwrap() error { return e.cause }

func checkPaywallPublishable(ctx context.Context, client *api.Client, projectID, paywallID string) ([]api.PaywallValidationIssue, error) {
	paywall, err := client.Paywalls.GetWithComponents(ctx, projectID, paywallID)
	switch {
	case api.IsUnauthorized(err):
		return nil, err
	case api.IsNotFound(err):
		return nil, nil
	case err != nil:
		return nil, validationUnavailable(err)
	}
	version := currentComponentsVersion(paywall.Components)
	if version == nil || presentJSON(version.ComponentsConfig) == nil {
		return nil, nil
	}

	var (
		wg                                sync.WaitGroup
		offering                          *api.Offering
		apps                              *api.Page[api.App]
		uiConfig                          json.RawMessage
		offeringErr, appsErr, uiConfigErr error
	)
	if paywall.OfferingID != "" {
		wg.Go(func() { offering, offeringErr = client.Offerings.GetWithProducts(ctx, projectID, paywall.OfferingID) })
		wg.Go(func() { apps, appsErr = client.Apps.List(ctx, projectID) })
	}
	wg.Go(func() { uiConfig, uiConfigErr = client.Projects.UIConfig(ctx, projectID) })
	wg.Wait()
	for _, err := range []error{offeringErr, appsErr, uiConfigErr} {
		if api.IsUnauthenticated(err) {
			return nil, err
		}
	}
	var appTypes map[string]string
	if offeringErr != nil || appsErr != nil || !isCompleteOffering(offering) {
		offering = nil
	} else {
		appTypes = make(map[string]string, len(apps.Items))
		for _, app := range apps.Items {
			appTypes[app.ID] = string(app.Type)
		}
	}

	result, err := client.Paywalls.Validate(ctx, projectID, buildPaywallValidationRequest(paywall, version, offering, appTypes, uiConfig))
	if api.IsUnauthorized(err) {
		return nil, err
	}
	if err != nil {
		return nil, validationUnavailable(err)
	}

	var errs, warnings []api.PaywallValidationIssue
	for _, issue := range result.Issues {
		switch issue.Severity {
		case "error":
			errs = append(errs, issue)
		case "warning":
			warnings = append(warnings, issue)
		}
	}
	if len(errs) > 0 {
		return nil, WithHint(&paywallValidationError{issues: errs}, "Fix the errors in the dashboard builder or with `rc paywalls ai`, then publish again.")
	}
	if !result.Valid {
		return nil, validationUnavailable(errors.New("the validator rejected the paywall without listing an error"))
	}
	return warnings, nil
}

func buildPaywallValidationRequest(paywall *api.Paywall, version *api.PaywallComponentsVersion, offering *api.Offering, appTypes map[string]string, uiConfig json.RawMessage) api.PaywallValidationRequest {
	req := api.PaywallValidationRequest{
		Paywall: api.PaywallValidationPaywall{
			RCPublicID:              paywall.ID,
			Name:                    ptrIfSet(paywall.Name),
			TemplateName:            "components",
			OfferingID:              ptrIfSet(paywall.OfferingID),
			DefaultLocale:           version.DefaultLocale,
			ComponentsConfig:        orEmptyObject(version.ComponentsConfig),
			ComponentsLocalizations: version.ComponentsLocalizations,
			StateDeclarations:       orEmptyObject(version.StateDeclarations),
		},
		Context: api.PaywallValidationContext{UIConfig: uiConfig},
	}
	if offering != nil {
		req.Context.Offering = validationOffering(offering, appTypes)
	}
	return req
}

func validationOffering(offering *api.Offering, appTypes map[string]string) *api.PaywallValidationOffering {
	out := &api.PaywallValidationOffering{
		ID:          offering.ID,
		Identifier:  offering.LookupKey,
		DisplayName: offering.DisplayName,
		IsCurrent:   offering.IsCurrent,
		Packages:    []api.PaywallValidationPackage{},
	}
	if offering.Packages == nil {
		return out
	}
	for _, pkg := range offering.Packages.Items {
		products := []api.PaywallValidationProduct{}
		if pkg.Products != nil {
			for _, association := range pkg.Products.Items {
				product := api.PaywallValidationProduct{ID: association.Product.ID}
				if appType, ok := appTypes[association.Product.AppID]; ok {
					product.App = &api.PaywallValidationAppRef{Type: appType}
				}
				products = append(products, product)
			}
		}
		out.Packages = append(out.Packages, api.PaywallValidationPackage{
			ID:          pkg.ID,
			Identifier:  pkg.LookupKey,
			DisplayName: pkg.DisplayName,
			Products:    products,
		})
	}
	return out
}

func isCompleteOffering(offering *api.Offering) bool {
	if offering == nil {
		return false
	}
	if offering.Packages == nil {
		return true
	}
	if offering.Packages.NextPage != nil && *offering.Packages.NextPage != "" {
		return false
	}
	for _, pkg := range offering.Packages.Items {
		if pkg.Products != nil && pkg.Products.NextPage != nil && *pkg.Products.NextPage != "" {
			return false
		}
	}
	return true
}

func describeValidationIssue(issue api.PaywallValidationIssue) string {
	var where []string
	if issue.ComponentID != "" {
		where = append(where, "component "+issue.ComponentID)
	}
	if issue.Property != "" {
		where = append(where, "property "+issue.Property)
	}
	if issue.Locale != "" {
		where = append(where, "locale "+issue.Locale)
	}
	if issue.Path != "" {
		where = append(where, "path "+issue.Path)
	}
	line := fmt.Sprintf("%s [%s]", issue.Message, issue.Key)
	if len(where) > 0 {
		line = strings.Join(where, ", ") + ": " + line
	}
	return output.SanitizeLine(line)
}

func currentComponentsVersion(components *api.PaywallComponents) *api.PaywallComponentsVersion {
	if components == nil {
		return nil
	}
	if components.Draft != nil {
		return components.Draft
	}
	return components.Published
}

func validationUnavailable(cause error) error {
	return WithHint(&paywallValidationUnavailableError{cause: cause}, "Try again shortly, or publish from the dashboard.")
}

func orEmptyObject(raw json.RawMessage) json.RawMessage {
	if presentJSON(raw) == nil {
		return json.RawMessage("{}")
	}
	return raw
}
