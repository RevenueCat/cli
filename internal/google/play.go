package google

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/oauth2"
	androidpublisher "google.golang.org/api/androidpublisher/v3"
	"google.golang.org/api/option"
)

// PlayAppPermissions are the per-app (package-scoped) grant permissions
// RevenueCat needs. All three exist at the app level, so we never grant
// account-wide access. Maps to the Play Console labels:
//   - CAN_VIEW_NON_FINANCIAL_DATA → "View app information"
//   - CAN_VIEW_FINANCIAL_DATA     → "View financial data, orders, and surveys"
//   - CAN_MANAGE_ORDERS           → "Manage orders and subscriptions"
var PlayAppPermissions = []string{
	"CAN_VIEW_NON_FINANCIAL_DATA",
	"CAN_VIEW_FINANCIAL_DATA",
	"CAN_MANAGE_ORDERS",
}

// developerIDPattern extracts the numeric Play developer account ID from a Play
// Console URL (…/developers/{id}/…) or accepts a bare ID.
var developerIDPattern = regexp.MustCompile(`developers/(\d{15,25})`)

// ParseDeveloperID normalizes a pasted Play Console URL or raw account ID into
// the numeric developer ID. Google exposes no API to discover this, so the
// human must supply it once.
func ParseDeveloperID(input string) (string, error) {
	input = strings.TrimSpace(input)
	if m := developerIDPattern.FindStringSubmatch(input); m != nil {
		return m[1], nil
	}
	if regexp.MustCompile(`^\d{15,25}$`).MatchString(input) {
		return input, nil
	}
	return "", fmt.Errorf("couldn't find a Play developer account ID in %q — paste your Play Console URL (it contains …/developers/NUMBER/…) or the numeric ID", input)
}

// PlayResult reports what AddServiceAccountToPlay did, for idempotent messaging.
type PlayResult struct {
	UserCreated  bool
	GrantCreated bool
	GrantUpdated bool
}

// AddServiceAccountToPlay invites the service account to the Play developer
// account (if not already a user) and grants it the package-scoped permissions
// (creating or updating the grant as needed). Idempotent.
//
// Requires the signed-in human to be a Play Console Owner/Admin.
func AddServiceAccountToPlay(ctx context.Context, ts oauth2.TokenSource, developerID, saEmail, packageName, quotaProject string) (*PlayResult, error) {
	opts := []option.ClientOption{option.WithTokenSource(ts)}
	if quotaProject != "" {
		// Bill Android Publisher against the user's own project (where the API
		// is enabled), not the OAuth client's project.
		opts = append(opts, option.WithQuotaProject(quotaProject))
	}
	svc, err := androidpublisher.NewService(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("android publisher client: %w", err)
	}
	parent := "developers/" + developerID

	existing, err := findUser(ctx, svc, parent, saEmail)
	if err != nil {
		return nil, err
	}

	result := &PlayResult{}
	if existing == nil {
		if _, err := svc.Users.Create(parent, &androidpublisher.User{Email: saEmail}).Context(ctx).Do(); err != nil {
			return nil, fmt.Errorf("add %s to Play developer account: %w", saEmail, err)
		}
		result.UserCreated = true
	}

	// Does the user already have a grant for this package with our permissions?
	grantName := fmt.Sprintf("%s/users/%s/grants/%s", parent, saEmail, packageName)
	if existing != nil {
		for _, g := range existing.Grants {
			if grantPackage(g.Name) == packageName {
				if hasAllPermissions(g.AppLevelPermissions) {
					return result, nil // fully configured
				}
				g.AppLevelPermissions = PlayAppPermissions
				if _, err := svc.Grants.Patch(grantName, g).UpdateMask("appLevelPermissions").Context(ctx).Do(); err != nil {
					return nil, fmt.Errorf("update Play permissions for %s: %w", packageName, err)
				}
				result.GrantUpdated = true
				return result, nil
			}
		}
	}

	grantParent := fmt.Sprintf("%s/users/%s", parent, saEmail)
	_, err = svc.Grants.Create(grantParent, &androidpublisher.Grant{
		PackageName:         packageName,
		AppLevelPermissions: PlayAppPermissions,
	}).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("grant Play access to %s: %w", packageName, err)
	}
	result.GrantCreated = true
	return result, nil
}

func findUser(ctx context.Context, svc *androidpublisher.Service, parent, email string) (*androidpublisher.User, error) {
	var found *androidpublisher.User
	call := svc.Users.List(parent)
	err := call.Pages(ctx, func(page *androidpublisher.ListUsersResponse) error {
		for _, u := range page.Users {
			if strings.EqualFold(u.Email, email) {
				found = u
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list Play users: %w", err)
	}
	return found, nil
}

func grantPackage(grantName string) string {
	i := strings.LastIndex(grantName, "/grants/")
	if i < 0 {
		return ""
	}
	return grantName[i+len("/grants/"):]
}

func hasAllPermissions(have []string) bool {
	set := make(map[string]bool, len(have))
	for _, p := range have {
		set[p] = true
	}
	for _, want := range PlayAppPermissions {
		if !set[want] {
			return false
		}
	}
	return true
}
