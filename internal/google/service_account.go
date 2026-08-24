package google

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/oauth2"
	cloudresourcemanager "google.golang.org/api/cloudresourcemanager/v3"
	"google.golang.org/api/googleapi"
	iam "google.golang.org/api/iam/v1"
	"google.golang.org/api/option"
)

const (
	// SAAccountID is the account-id portion of the service account RevenueCat
	// uses; the full email is {SAAccountID}@{projectID}.iam.gserviceaccount.com.
	SAAccountID   = "revenuecat-service-account"
	SADisplayName = "RevenueCat"
)

// ProjectRoles are the Google Cloud IAM roles the service account needs.
// Matches RevenueCat's current requirements.
var ProjectRoles = []string{"roles/pubsub.editor", "roles/monitoring.viewer"}

// ServiceAccountEmail is the deterministic email for the given project.
func ServiceAccountEmail(projectID string) string {
	return fmt.Sprintf("%s@%s.iam.gserviceaccount.com", SAAccountID, projectID)
}

// EnsureServiceAccount returns the RevenueCat service account, creating it if it
// doesn't exist. Idempotent: an existing account is returned without error.
// The bool reports whether it was newly created (for user-facing messaging).
func EnsureServiceAccount(ctx context.Context, ts oauth2.TokenSource, projectID string) (created bool, email string, err error) {
	svc, err := iam.NewService(ctx, option.WithTokenSource(ts), option.WithQuotaProject(projectID))
	if err != nil {
		return false, "", fmt.Errorf("iam client: %w", err)
	}
	email = ServiceAccountEmail(projectID)
	name := fmt.Sprintf("projects/%s/serviceAccounts/%s", projectID, email)

	if _, getErr := svc.Projects.ServiceAccounts.Get(name).Context(ctx).Do(); getErr == nil {
		return false, email, nil // already exists
	} else if !isNotFound(getErr) {
		return false, "", fmt.Errorf("look up service account: %w", classifyOrgPolicy(getErr))
	}

	_, createErr := svc.Projects.ServiceAccounts.Create("projects/"+projectID, &iam.CreateServiceAccountRequest{
		AccountId:      SAAccountID,
		ServiceAccount: &iam.ServiceAccount{DisplayName: SADisplayName},
	}).Context(ctx).Do()
	if createErr != nil {
		return false, "", fmt.Errorf("create service account: %w", classifyOrgPolicy(createErr))
	}
	return true, email, nil
}

// GrantProjectRoles ensures the service account holds ProjectRoles on the
// project. Read-modify-write on the project IAM policy; idempotent (roles the
// member already has are skipped). Returns the roles it actually added.
func GrantProjectRoles(ctx context.Context, ts oauth2.TokenSource, projectID, saEmail string) ([]string, error) {
	crm, err := cloudresourcemanager.NewService(ctx, option.WithTokenSource(ts), option.WithQuotaProject(projectID))
	if err != nil {
		return nil, fmt.Errorf("cloud resource manager client: %w", err)
	}
	resource := "projects/" + projectID
	// Request policy version 3 so conditional bindings come back intact — a v1
	// read can represent them incompletely, and writing that back would drop the
	// conditions on any existing conditional bindings.
	policy, err := crm.Projects.GetIamPolicy(resource, &cloudresourcemanager.GetIamPolicyRequest{
		Options: &cloudresourcemanager.GetPolicyOptions{RequestedPolicyVersion: 3},
	}).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("get IAM policy: %w", classifyOrgPolicy(err))
	}
	member := "serviceAccount:" + saEmail
	var added []string
	for _, role := range ProjectRoles {
		if addMember(policy, role, member) {
			added = append(added, role)
		}
	}
	if len(added) == 0 {
		return nil, nil // nothing to change
	}
	_, err = crm.Projects.SetIamPolicy(resource, &cloudresourcemanager.SetIamPolicyRequest{Policy: policy}).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("set IAM policy: %w", classifyOrgPolicy(err))
	}
	return added, nil
}

// addMember grants member the role via an unconditional binding, creating one if
// needed. Returns true if a change was made. Conditional bindings for the role
// are skipped when looking for one to reuse — appending to them would give the
// service account access limited by someone else's condition (e.g. an expiry),
// not the unconditional access this setup expects.
func addMember(policy *cloudresourcemanager.Policy, role, member string) bool {
	for _, b := range policy.Bindings {
		if b.Role != role || b.Condition != nil {
			continue
		}
		for _, m := range b.Members {
			if m == member {
				return false // already present, unconditionally
			}
		}
		b.Members = append(b.Members, member)
		return true
	}
	policy.Bindings = append(policy.Bindings, &cloudresourcemanager.Binding{Role: role, Members: []string{member}})
	return true
}

// PruneUserKeys deletes the user-managed keys on the RevenueCat service account,
// skipping keepKeyName, and reports how many it removed. This keeps re-runs from
// piling keys up toward Google's ~10-per-account limit. It only ever runs against
// the rc-owned service account, and only touches USER_MANAGED keys — Google's
// SYSTEM_MANAGED signing keys are never listed or deleted. Pass the newly created
// key's name as keepKeyName so pruning never deletes the credential just issued.
func PruneUserKeys(ctx context.Context, ts oauth2.TokenSource, projectID, saEmail, keepKeyName string) (int, error) {
	svc, err := iam.NewService(ctx, option.WithTokenSource(ts), option.WithQuotaProject(projectID))
	if err != nil {
		return 0, fmt.Errorf("iam client: %w", err)
	}
	name := fmt.Sprintf("projects/%s/serviceAccounts/%s", projectID, saEmail)
	resp, err := svc.Projects.ServiceAccounts.Keys.List(name).KeyTypes("USER_MANAGED").Context(ctx).Do()
	if err != nil {
		return 0, fmt.Errorf("list service account keys: %w", err)
	}
	removed := 0
	for _, k := range resp.Keys {
		if k.Name == keepKeyName {
			continue
		}
		if _, err := svc.Projects.ServiceAccounts.Keys.Delete(k.Name).Context(ctx).Do(); err != nil {
			return removed, fmt.Errorf("delete service account key: %w", err)
		}
		removed++
	}
	return removed, nil
}

// CreateKey creates a JSON service-account key and returns the decoded key file
// bytes and the key's resource name. The private key material is returned inline
// by Google (only on create) so it never touches disk here — the caller keeps it
// in memory. The name lets the caller prune older keys without deleting this one.
func CreateKey(ctx context.Context, ts oauth2.TokenSource, projectID, saEmail string) ([]byte, string, error) {
	svc, err := iam.NewService(ctx, option.WithTokenSource(ts), option.WithQuotaProject(projectID))
	if err != nil {
		return nil, "", fmt.Errorf("iam client: %w", err)
	}
	name := fmt.Sprintf("projects/%s/serviceAccounts/%s", projectID, saEmail)
	key, err := svc.Projects.ServiceAccounts.Keys.Create(name, &iam.CreateServiceAccountKeyRequest{}).Context(ctx).Do()
	if err != nil {
		return nil, "", fmt.Errorf("create service account key: %w", classifyOrgPolicy(err))
	}
	data, err := base64.StdEncoding.DecodeString(key.PrivateKeyData)
	if err != nil {
		return nil, "", fmt.Errorf("decode key material: %w", err)
	}
	return data, key.Name, nil
}

func isNotFound(err error) bool {
	var apiErr *googleapi.Error
	return errors.As(err, &apiErr) && apiErr.Code == 404
}

// OrgPolicyError signals that a Google Cloud organization policy blocked the
// operation, so the CLI can print actionable guidance instead of a raw 400.
type OrgPolicyError struct {
	Constraint string // e.g. "iam.disableServiceAccountKeyCreation"
	err        error
}

func (e *OrgPolicyError) Error() string {
	return fmt.Sprintf("blocked by Google Cloud organization policy %q: %v", e.Constraint, e.err)
}
func (e *OrgPolicyError) Unwrap() error { return e.err }

// TosError signals that the signed-in account hasn't accepted a required
// Google API terms of service (e.g. Android Publisher), which blocks enabling
// those APIs until the human accepts them once in the Console.
type TosError struct {
	URL string // the terms-acceptance page to visit
	err error
}

func (e *TosError) Error() string {
	return fmt.Sprintf("a Google API terms of service must be accepted at %s: %v", e.URL, e.err)
}
func (e *TosError) Unwrap() error { return e.err }

var tosURLPattern = regexp.MustCompile(`https://[^\s)]+/terms/[^\s)]+`)

// classifyTos maps Google's UREQ_TOS_NOT_ACCEPTED response into a TosError
// carrying the acceptance URL. Other errors pass through unchanged.
func classifyTos(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *googleapi.Error
	if !errors.As(err, &apiErr) {
		return err
	}
	msg := apiErr.Message
	if !strings.Contains(msg, "terms of service") && !strings.Contains(strings.ToUpper(msg), "TOS") {
		return err
	}
	url := "https://console.developers.google.com/terms"
	if m := tosURLPattern.FindString(msg); m != "" {
		url = m
	}
	return &TosError{URL: url, err: err}
}

// classifyOrgPolicy maps Google's FAILED_PRECONDITION / 400 responses for the
// common service-account org-policy constraints into an OrgPolicyError. Other
// errors pass through unchanged.
func classifyOrgPolicy(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *googleapi.Error
	if !errors.As(err, &apiErr) {
		return err
	}
	msg := strings.ToLower(apiErr.Message)
	switch {
	case strings.Contains(msg, "disableserviceaccountkeycreation") || strings.Contains(msg, "key creation is not allowed"):
		return &OrgPolicyError{Constraint: "iam.disableServiceAccountKeyCreation", err: err}
	case strings.Contains(msg, "disableserviceaccountcreation") || strings.Contains(msg, "service account creation is not allowed"):
		return &OrgPolicyError{Constraint: "iam.disableServiceAccountCreation", err: err}
	}
	return err
}
