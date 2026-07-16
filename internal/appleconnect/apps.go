package appleconnect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
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

// RegisterBundleID registers an explicit iOS bundle ID in the Apple Developer
// Portal. An already-registered identifier is not an error.
func (c *Client) RegisterBundleID(ctx context.Context, session *Session, identifier, name string) error {
	if session == nil || session.client != c {
		return errors.New("authenticated Apple session is required")
	}
	body := map[string]any{
		"data": map[string]any{
			"type": "bundleIds",
			"attributes": map[string]any{
				"identifier": identifier,
				"name":       name,
				"platform":   "IOS",
			},
		},
	}
	err := c.doJSON(ctx, http.MethodPost, c.ascBaseURL+"/iris/v1/bundleIds", body, nil, irisHeaders(c.ascBaseURL))
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "already") {
		return nil
	}
	if err != nil {
		return fmt.Errorf("register bundle ID %s: %w", identifier, err)
	}
	return nil
}

// CreateApp creates the App Store Connect app record, mirroring fastlane
// produce: fetch the creation template, fill it in, and post it back.
func (c *Client) CreateApp(ctx context.Context, session *Session, name, bundleID, sku string) error {
	if session == nil || session.client != c {
		return errors.New("authenticated Apple session is required")
	}
	tunesBase := c.ascBaseURL + "/WebObjects/iTunesConnect.woa/ra"
	headers := irisHeaders(c.ascBaseURL)

	var template struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, tunesBase+"/apps/create/v2/?platformString=ios", nil, &template, headers); err != nil {
		return fmt.Errorf("fetch App Store Connect app creation template: %w", err)
	}
	if template.Data == nil {
		template.Data = map[string]json.RawMessage{}
	}
	setValue := func(key string, value any) {
		encoded, _ := json.Marshal(map[string]any{"value": value})
		template.Data[key] = encoded
	}
	setValue("name", name)
	setValue("bundleId", bundleID)
	setValue("primaryLanguage", "English")
	setValue("primaryLocaleCode", "en-US")
	setValue("vendorId", sku)
	setValue("bundleIdSuffix", nil)
	setValue("enabledPlatformsForCreation", []string{"ios"})
	template.Data["initialPlatform"], _ = json.Marshal("ios")

	var created struct {
		Data struct {
			SectionErrorKeys []string `json:"sectionErrorKeys"`
		} `json:"data"`
		Messages struct {
			Error []string `json:"error"`
		} `json:"messages"`
	}
	if err := c.doJSON(ctx, http.MethodPost, tunesBase+"/apps/create/v2", template.Data, &created, headers); err != nil {
		return fmt.Errorf("create App Store Connect app %s: %w", bundleID, err)
	}
	if issues := append(created.Messages.Error, created.Data.SectionErrorKeys...); len(issues) > 0 {
		return fmt.Errorf("create App Store Connect app %s: %s", bundleID, strings.Join(issues, "; "))
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
