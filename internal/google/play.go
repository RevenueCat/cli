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

// bootstrapPermission is the least-sensitive account-level permission, used
// only transiently: users.create needs at least one account permission, so we
// create with this, add the package grant, then remove it.
const bootstrapPermission = "CAN_VIEW_NON_FINANCIAL_DATA_GLOBAL"

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
	userName := fmt.Sprintf("%s/users/%s", parent, saEmail)
	grantParent := userName
	grantName := fmt.Sprintf("%s/grants/%s", userName, packageName)

	if existing == nil {
		// users.create requires at least one account-level permission (grants
		// are output-only and can't be set at creation). Bootstrap the user with
		// the least-sensitive account permission, add the package-scoped grant,
		// then strip the account permission — leaving least-privilege, per-app
		// access.
		if _, err := svc.Users.Create(parent, &androidpublisher.User{
			Email:                       saEmail,
			DeveloperAccountPermissions: []string{bootstrapPermission},
		}).Context(ctx).Do(); err != nil {
			return nil, fmt.Errorf("add %s to Play developer account: %w", saEmail, err)
		}
		result.UserCreated = true
		// Strip the bootstrap permission on the way out even if the grant below
		// fails — otherwise a failed run leaves the account-wide permission set.
		defer stripBootstrap(ctx, svc, userName)

		if _, err := svc.Grants.Create(grantParent, &androidpublisher.Grant{
			PackageName:         packageName,
			AppLevelPermissions: PlayAppPermissions,
		}).Context(ctx).Do(); err != nil {
			return nil, fmt.Errorf("grant Play access to %s: %w", packageName, err)
		}
		result.GrantCreated = true
		return result, nil
	}

	// Existing user: a prior interrupted run may have left our bootstrap
	// permission behind. Clean it up, but never touch account-level permissions
	// the user set up themselves (anything other than exactly our bootstrap).
	if onlyBootstrap(existing.DeveloperAccountPermissions) {
		defer stripBootstrap(ctx, svc, userName)
	}

	// Ensure the package grant has our permissions, leaving any account-level
	// permissions the user set up themselves untouched.
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
	if _, err := svc.Grants.Create(grantParent, &androidpublisher.Grant{
		PackageName:         packageName,
		AppLevelPermissions: PlayAppPermissions,
	}).Context(ctx).Do(); err != nil {
		return nil, fmt.Errorf("grant Play access to %s: %w", packageName, err)
	}
	result.GrantCreated = true
	return result, nil
}

// stripBootstrap best-effort clears the transient account-wide bootstrap
// permission. Access still works without it (read-only "view app info" at
// worst), so callers ignore failures rather than abort.
func stripBootstrap(ctx context.Context, svc *androidpublisher.Service, userName string) {
	_, _ = svc.Users.Patch(userName, &androidpublisher.User{
		DeveloperAccountPermissions: []string{},
		ForceSendFields:             []string{"DeveloperAccountPermissions"},
	}).UpdateMask("developerAccountPermissions").Context(ctx).Do()
}

func onlyBootstrap(perms []string) bool {
	return len(perms) == 1 && perms[0] == bootstrapPermission
}

func findUser(ctx context.Context, svc *androidpublisher.Service, parent, email string) (*androidpublisher.User, error) {
	// Android Publisher's users.list doesn't support pagination: it requires
	// pageSize = -1 and returns everything in one response. Using the generic
	// paginator sets a page size Google rejects with a 400.
	resp, err := svc.Users.List(parent).PageSize(-1).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("list Play users: %w", err)
	}
	for _, u := range resp.Users {
		if strings.EqualFold(u.Email, email) {
			return u, nil
		}
	}
	return nil, nil
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
