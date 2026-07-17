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

const portalV1BundleIDs = "https://developer.apple.com/services-account/v1/bundleIds"

// portalV1 sends one request to the modern provisioning surface, which under
// web-session auth is always a POST: reads carry X-HTTP-Method-Override: GET
// with the query encoded in the body (Spaceship's proxy_get/proxy_post).
func (c *Client) portalV1(ctx context.Context, methodOverride string, body any, csrf http.Header) ([]byte, http.Header, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, portalV1BundleIDs, strings.NewReader(string(payload)))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/vnd.api+json")
	req.Header.Set("Accept", "application/vnd.api+json, application/json")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	if methodOverride != "" {
		req.Header.Set("X-HTTP-Method-Override", methodOverride)
	}
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
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail := portalV1ErrorDetail(responseBody)
		return responseBody, resp.Header, fmt.Errorf("the Apple Developer Portal returned HTTP %d: %s", resp.StatusCode, detail)
	}
	return responseBody, resp.Header, nil
}

func portalV1ErrorDetail(body []byte) string {
	var envelope struct {
		Errors []struct {
			Title  string `json:"title"`
			Detail string `json:"detail"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && len(envelope.Errors) > 0 {
		parts := make([]string, 0, len(envelope.Errors))
		for _, e := range envelope.Errors {
			if e.Detail != "" {
				parts = append(parts, e.Detail)
			} else if e.Title != "" {
				parts = append(parts, e.Title)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "; ")
		}
	}
	return "no error detail"
}

// RegisterBundleID registers an explicit iOS bundle ID in the Developer
// Portal via the modern provisioning API. An already-registered identifier
// is not an error.
func (c *Client) RegisterBundleID(ctx context.Context, session *Session, identifier, name string) error {
	if session == nil || session.client != c {
		return errors.New("authenticated Apple session is required")
	}
	teamID, err := c.portalTeamID(ctx, session)
	if err != nil {
		return fmt.Errorf("register bundle ID %s: %w", identifier, err)
	}
	// The read primes CSRF tokens for the write and detects an existing
	// registration in one go.
	query := url.Values{
		"filter[identifier]": []string{identifier},
		"limit":              []string{"100"},
	}
	listBody := map[string]any{
		"urlEncodedQueryParams": query.Encode(),
		"teamId":                teamID,
	}
	existing, headers, err := c.portalV1(ctx, http.MethodGet, listBody, nil)
	if err != nil {
		return fmt.Errorf("register bundle ID %s: %w", identifier, err)
	}
	var list struct {
		Data []struct {
			Attributes struct {
				Identifier string `json:"identifier"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if json.Unmarshal(existing, &list) == nil {
		for _, item := range list.Data {
			if strings.EqualFold(item.Attributes.Identifier, identifier) {
				return nil // already registered
			}
		}
	}

	createBody := map[string]any{
		"data": map[string]any{
			"type": "bundleIds",
			"attributes": map[string]any{
				"name":       sanitizePortalName(name),
				"platform":   "IOS",
				"identifier": identifier,
				"seedId":     teamID,
				"teamId":     teamID,
			},
		},
	}
	if _, _, err := c.portalV1(ctx, "", createBody, headers); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "already") {
			return nil
		}
		return fmt.Errorf("register bundle ID %s: %w", identifier, err)
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
