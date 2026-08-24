package google

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/api/option"
	serviceusage "google.golang.org/api/serviceusage/v1"
)

// RequiredAPIs are the Google APIs RevenueCat's Play integration depends on.
// This matches RevenueCat's current Cloud Shell setup script.
var RequiredAPIs = []string{
	"cloudresourcemanager.googleapis.com",
	"iam.googleapis.com",
	"androidpublisher.googleapis.com",
	"playdeveloperreporting.googleapis.com",
	"pubsub.googleapis.com",
}

// EnableAPIs enables all RequiredAPIs on the project in a single batch call.
// batchEnable is idempotent — already-enabled services are a no-op — so re-runs
// are safe. (Google caps batchEnable at 20 service IDs; we have 5.)
func EnableAPIs(ctx context.Context, ts oauth2.TokenSource, projectID string) error {
	// NOTE: no WithQuotaProject here on purpose. batchEnable itself uses the
	// Service Usage API, which a brand-new target project doesn't have enabled
	// yet — so it can't bootstrap itself. Bill this one call against the OAuth
	// client's project (which has Service Usage enabled); it still enables the
	// services ON the target project. Every call AFTER enablement bills to the
	// target (where the APIs are now on).
	svc, err := serviceusage.NewService(ctx, option.WithTokenSource(ts))
	if err != nil {
		return fmt.Errorf("service usage client: %w", err)
	}
	parent := "projects/" + projectID
	req := &serviceusage.BatchEnableServicesRequest{ServiceIds: RequiredAPIs}
	op, err := svc.Services.BatchEnable(parent, req).Context(ctx).Do()
	if err != nil {
		if tos := classifyTos(err); tos != err {
			return fmt.Errorf("enable APIs: %w", tos)
		}
		return fmt.Errorf("enable APIs: %w", classifyOrgPolicy(err))
	}
	// Wait for enablement to finish — the immediate service-account and Play
	// steps that follow will fail on a fresh project if the APIs aren't on yet.
	return waitForEnable(ctx, svc, op)
}

// waitForEnable polls the batchEnable operation until it reports Done, so the
// caller doesn't race ahead of a still-enabling project.
func waitForEnable(ctx context.Context, svc *serviceusage.Service, op *serviceusage.Operation) error {
	for !op.Done {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
		got, err := svc.Operations.Get(op.Name).Context(ctx).Do()
		if err != nil {
			return fmt.Errorf("enable APIs: %w", err)
		}
		op = got
	}
	if op.Error != nil {
		return fmt.Errorf("enable APIs: %s", op.Error.Message)
	}
	return nil
}
