package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const DefaultSDKBaseURL = "https://api.revenuecat.com/v1"

type SDKService struct {
	baseURL   *url.URL
	http      *http.Client
	userAgent string
}

type SimulatedPurchase struct {
	FetchToken       string `json:"fetch_token"`
	AppUserID        string `json:"app_user_id"`
	ProductID        string `json:"product_id"`
	InitiationSource string `json:"initiation_source"`
	SDKOriginated    bool   `json:"sdk_originated"`
}

func NewSDKService(v2BaseURL string, httpClient *http.Client, userAgent string) *SDKService {
	base := v2BaseURL
	if base == "" || base == DefaultBaseURL {
		base = DefaultSDKBaseURL
	} else {
		base = strings.TrimSuffix(base, "/")
		if strings.HasSuffix(base, "/v2") {
			base = strings.TrimSuffix(base, "/v2") + "/v1"
		}
	}
	u, _ := url.Parse(base)
	if u.Path == "" {
		u.Path = "/v1"
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	if userAgent == "" {
		userAgent = "revenuecat-cli/dev"
	}
	return &SDKService{baseURL: u, http: httpClient, userAgent: userAgent}
}

func (s *SDKService) Offerings(ctx context.Context, publicAPIKey, appUserID string) (json.RawMessage, error) {
	u := *s.baseURL
	basePath := strings.TrimSuffix(u.Path, "/")
	baseRawPath := strings.TrimSuffix(u.EscapedPath(), "/")
	u.Path = basePath + "/subscribers/" + appUserID + "/offerings"
	u.RawPath = baseRawPath + "/subscribers/" + url.PathEscape(appUserID) + "/offerings"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+publicAPIKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", s.userAgent)
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, parseError(resp)
	}
	body, err := io.ReadAll(resp.Body)
	return json.RawMessage(body), err
}

func (s *SDKService) SimulatePurchase(ctx context.Context, publicAPIKey string, purchase SimulatedPurchase) (json.RawMessage, error) {
	body, err := json.Marshal(purchase)
	if err != nil {
		return nil, err
	}
	u := *s.baseURL
	u.Path = strings.TrimSuffix(u.Path, "/") + "/receipts"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+publicAPIKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", s.userAgent)
	req.Header.Set("X-Platform", "iOS")
	req.Header.Set("X-Version", "rc-cli")
	req.Header.Set("X-Client-Bundle-Id", "com.revenuecat.cli")
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, parseError(resp)
	}
	responseBody, err := io.ReadAll(resp.Body)
	return json.RawMessage(responseBody), err
}
