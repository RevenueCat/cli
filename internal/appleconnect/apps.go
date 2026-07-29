package appleconnect

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// AppExists reports whether an App Store Connect app record exists for the
// bundle ID on the selected team.
func (c *Client) AppExists(ctx context.Context, session *Session, bundleID string) (bool, error) {
	if session == nil || session.client != c {
		return false, errors.New("authenticated Apple session is required")
	}
	query := url.Values{
		"filter[bundleId]": []string{bundleID},
		"limit":            []string{"1"},
	}
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	endpoint := c.ascBaseURL + "/iris/v1/apps?" + query.Encode()
	if err := c.doJSON(ctx, http.MethodGet, endpoint, nil, &out, irisHeaders(c.ascBaseURL)); err != nil {
		return false, fmt.Errorf("look up App Store Connect app for %s: %w", bundleID, err)
	}
	return len(out.Data) > 0, nil
}

// CreateApp creates the App Store Connect app record via the modern iris
// API, mirroring Spaceship's ConnectAPI::Tunes#post_app: one JSON:API graph
// of app, appInfo, localizations, and an initial iOS version, wired together
// with ${placeholder} IDs the server resolves.
func (c *Client) CreateApp(ctx context.Context, session *Session, name, bundleID, sku string) error {
	if session == nil || session.client != c {
		return errors.New("authenticated Apple session is required")
	}
	const locale = "en-US"
	body := map[string]any{
		"data": map[string]any{
			"type": "apps",
			"attributes": map[string]any{
				"sku":           sku,
				"primaryLocale": locale,
				"bundleId":      bundleID,
			},
			"relationships": map[string]any{
				"appStoreVersions": map[string]any{
					"data": []any{map[string]any{"type": "appStoreVersions", "id": "${store-version-IOS}"}},
				},
				"appInfos": map[string]any{
					"data": []any{map[string]any{"type": "appInfos", "id": "${new-appInfo-id}"}},
				},
			},
		},
		"included": []any{
			map[string]any{
				"type": "appInfos",
				"id":   "${new-appInfo-id}",
				"relationships": map[string]any{
					"appInfoLocalizations": map[string]any{
						"data": []any{map[string]any{"type": "appInfoLocalizations", "id": "${new-appInfoLocalization-id}"}},
					},
				},
			},
			map[string]any{
				"type":       "appInfoLocalizations",
				"id":         "${new-appInfoLocalization-id}",
				"attributes": map[string]any{"locale": locale, "name": name},
			},
			map[string]any{
				"type":       "appStoreVersions",
				"id":         "${store-version-IOS}",
				"attributes": map[string]any{"platform": "IOS", "versionString": "1.0"},
				"relationships": map[string]any{
					"appStoreVersionLocalizations": map[string]any{
						"data": []any{map[string]any{"type": "appStoreVersionLocalizations", "id": "${new-IOSVersionLocalization-id}"}},
					},
				},
			},
			map[string]any{
				"type":       "appStoreVersionLocalizations",
				"id":         "${new-IOSVersionLocalization-id}",
				"attributes": map[string]any{"locale": locale},
			},
		},
	}
	if err := c.doJSON(ctx, http.MethodPost, c.ascBaseURL+"/iris/v1/apps", body, nil, irisHeaders(c.ascBaseURL)); err != nil {
		return fmt.Errorf("create App Store Connect app %s: %w", bundleID, err)
	}
	return nil
}

func irisHeaders(ascBaseURL string) http.Header {
	return http.Header{
		"Accept":       []string{"application/vnd.api+json, application/json"},
		"Content-Type": []string{"application/json"},
		"Origin":       []string{ascBaseURL},
		"Referer":      []string{ascBaseURL + "/"},
		"X-CSRF-ITC":   []string{"[asc-ui]"},
	}
}
