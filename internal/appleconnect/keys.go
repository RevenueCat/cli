package appleconnect

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type KeyKind string

const (
	AppStoreConnectKey KeyKind = "app_store_connect"
	InAppPurchaseKey   KeyKind = "in_app_purchase"
)

type Key struct {
	Kind       KeyKind `json:"kind"`
	ID         string  `json:"id"`
	IssuerID   string  `json:"issuer_id"`
	PrivateKey string  `json:"-"`
}

func (c *Client) CreateAppStoreConnectKey(ctx context.Context, session *Session, name string) (*Key, error) {
	body := map[string]any{
		"data": map[string]any{
			"type": "apiKeys",
			"attributes": map[string]any{
				"nickname":       name,
				"roles":          []string{"APP_MANAGER"},
				"allAppsVisible": true,
				"keyType":        "PUBLIC_API",
			},
		},
	}
	return c.createKey(ctx, session, AppStoreConnectKey, "/iris/v1/apiKeys", "apiKeys", body)
}

func (c *Client) CreateInAppPurchaseKey(ctx context.Context, session *Session, name string) (*Key, error) {
	body := map[string]any{
		"data": map[string]any{
			"type":       "subscriptionKeys",
			"attributes": map[string]any{"nickname": name},
		},
	}
	return c.createKey(ctx, session, InAppPurchaseKey, "/iris/v1/subscriptionKeys", "subscriptionKeys", body)
}

func (c *Client) createKey(ctx context.Context, session *Session, kind KeyKind, path, resourceType string, requestBody any) (*Key, error) {
	if session == nil || session.client != c {
		return nil, errors.New("authenticated Apple session is required")
	}
	if strings.TrimSpace(session.Provider.PublicID) == "" {
		return nil, errors.New("App Store Connect session did not expose an issuer ID")
	}
	headers := http.Header{
		"Accept":       []string{"application/vnd.api+json, application/json"},
		"Content-Type": []string{"application/json"},
		"Origin":       []string{c.ascBaseURL},
		"Referer":      []string{c.ascBaseURL + "/access/integrations/api"},
		"X-CSRF-ITC":   []string{"[asc-ui]"},
	}
	var created struct {
		Data struct {
			ID         string `json:"id"`
			Attributes struct {
				CanDownload bool `json:"canDownload"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodPost, c.ascBaseURL+path, requestBody, &created, headers); err != nil {
		return nil, fmt.Errorf("create Apple %s key: %w", kind, err)
	}
	if created.Data.ID == "" {
		return nil, fmt.Errorf("Apple %s key response did not include an ID", kind)
	}
	if !created.Data.Attributes.CanDownload {
		return nil, fmt.Errorf("Apple created %s key %s but did not permit its one-time download", kind, created.Data.ID)
	}
	query := url.Values{"fields[" + resourceType + "]": []string{"privateKey"}}
	var downloaded struct {
		Data struct {
			Attributes struct {
				PrivateKey string `json:"privateKey"`
			} `json:"attributes"`
		} `json:"data"`
	}
	endpoint := c.ascBaseURL + path + "/" + url.PathEscape(created.Data.ID) + "?" + query.Encode()
	if err := c.doJSON(ctx, http.MethodGet, endpoint, nil, &downloaded, headers); err != nil {
		return nil, fmt.Errorf("download Apple %s key %s: %w", kind, created.Data.ID, err)
	}
	privateKey, err := decodePrivateKey(downloaded.Data.Attributes.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("decode Apple %s key %s: %w", kind, created.Data.ID, err)
	}
	return &Key{Kind: kind, ID: created.Data.ID, IssuerID: session.Provider.PublicID, PrivateKey: privateKey}, nil
}

func decodePrivateKey(value string) (string, error) {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "BEGIN PRIVATE KEY") || strings.Contains(value, "BEGIN EC PRIVATE KEY") {
		return value + "\n", nil
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return "", err
	}
	var quoted string
	if json.Unmarshal(decoded, &quoted) == nil && strings.Contains(quoted, "PRIVATE KEY") {
		return strings.TrimSpace(quoted) + "\n", nil
	}
	pem := strings.TrimSpace(string(decoded))
	if !strings.Contains(pem, "PRIVATE KEY") {
		return "", errors.New("downloaded value is not a PEM private key")
	}
	return pem + "\n", nil
}
