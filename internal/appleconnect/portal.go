package appleconnect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// The Apple Developer Portal (developer.apple.com) owns bundle IDs; App
// Store Connect does not expose them to session auth. These are the same
// form endpoints fastlane's Spaceship::Portal uses, authenticated by the
// shared .apple.com session cookies.
const portalBaseURL = "https://developer.apple.com/services-account/QH65B2"

type portalEnvelope struct {
	ResultCode   int    `json:"resultCode"`
	ResultString string `json:"resultString"`
	UserString   string `json:"userString"`
	Teams        []struct {
		TeamID string `json:"teamId"`
		Name   string `json:"name"`
	} `json:"teams"`
}

// portalForm posts a form-encoded portal action, echoing any CSRF tokens,
// and returns the parsed envelope plus response headers (for CSRF capture).
func (c *Client) portalForm(ctx context.Context, path string, form url.Values, csrf http.Header) (*portalEnvelope, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, portalBaseURL+path, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	for _, key := range []string{"csrf", "csrf_ts"} {
		if value := csrf.Get(key); value != "" {
			req.Header.Set(key, value)
		}
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("calling the Apple Developer Portal: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, fmt.Errorf("the Apple Developer Portal returned HTTP %d for %s", resp.StatusCode, path)
	}
	var envelope portalEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, nil, fmt.Errorf("decoding Apple Developer Portal response for %s: %w", path, err)
	}
	return &envelope, resp.Header, nil
}

// portalTeamID resolves the Developer Portal team matching the selected App
// Store Connect provider (by name), or the only team on the account.
func (c *Client) portalTeamID(ctx context.Context, session *Session) (string, error) {
	envelope, _, err := c.portalForm(ctx, "/account/listTeams.action", url.Values{}, nil)
	if err != nil {
		return "", err
	}
	if len(envelope.Teams) == 0 {
		return "", errors.New("the Apple Developer Portal returned no teams for this account")
	}
	if len(envelope.Teams) == 1 {
		return envelope.Teams[0].TeamID, nil
	}
	for _, team := range envelope.Teams {
		if strings.EqualFold(team.Name, session.Provider.Name) {
			return team.TeamID, nil
		}
	}
	names := make([]string, len(envelope.Teams))
	for i, team := range envelope.Teams {
		names[i] = fmt.Sprintf("%s (%s)", team.Name, team.TeamID)
	}
	return "", fmt.Errorf("could not match App Store Connect team %q to a Developer Portal team; available: %s",
		session.Provider.Name, strings.Join(names, ", "))
}

// RegisterBundleID registers an explicit iOS bundle ID in the Developer
// Portal. An already-registered identifier is not an error.
func (c *Client) RegisterBundleID(ctx context.Context, session *Session, identifier, name string) error {
	if session == nil || session.client != c {
		return errors.New("authenticated Apple session is required")
	}
	teamID, err := c.portalTeamID(ctx, session)
	if err != nil {
		return fmt.Errorf("register bundle ID %s: %w", identifier, err)
	}
	// A list call primes the CSRF tokens the mutation endpoint requires.
	listForm := url.Values{
		"teamId":     []string{teamID},
		"pageSize":   []string{"1"},
		"pageNumber": []string{"1"},
		"sort":       []string{"name=asc"},
	}
	_, headers, err := c.portalForm(ctx, "/account/ios/identifiers/listAppIds.action", listForm, nil)
	if err != nil {
		return fmt.Errorf("register bundle ID %s: %w", identifier, err)
	}
	addForm := url.Values{
		"teamId":        []string{teamID},
		"name":          []string{sanitizePortalName(name)},
		"type":          []string{"explicit"},
		"identifier":    []string{identifier},
		"inAppPurchase": []string{"on"},
	}
	envelope, _, err := c.portalForm(ctx, "/account/ios/identifiers/addAppId.action", addForm, headers)
	if err != nil {
		return fmt.Errorf("register bundle ID %s: %w", identifier, err)
	}
	if envelope.ResultCode != 0 {
		message := envelope.UserString
		if message == "" {
			message = envelope.ResultString
		}
		if strings.Contains(strings.ToLower(message), "already") {
			return nil
		}
		return fmt.Errorf("register bundle ID %s: %s", identifier, message)
	}
	return nil
}

// sanitizePortalName strips characters the portal rejects in App ID names,
// mirroring Spaceship's valid_name_for.
func sanitizePortalName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == ' ' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "App"
	}
	return b.String()
}
