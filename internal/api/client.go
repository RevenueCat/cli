// Package api is a typed client for the RevenueCat v2 REST API.
//
// Design rules for this package:
//   - One file per resource (customers.go, projects.go, ...).
//   - Methods map 1:1 to REST endpoints; no compositing here.
//   - No CLI concepts (flags, output formats, prompts) leak in. Anything that
//     calls multiple endpoints to build a user-facing view belongs in the cli/
//     package, not here.
//   - All requests take context.Context.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const DefaultBaseURL = "https://api.revenuecat.com/v2"

type Options struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
	UserAgent  string
}

type Client struct {
	baseURL   *url.URL
	apiKey    string
	http      *http.Client
	userAgent string

	Projects      *ProjectsService
	Customers     *CustomersService
	Entitlements  *EntitlementsService
	Offerings     *OfferingsService
	Packages      *PackagesService
	Products      *ProductsService
	Subscriptions *SubscriptionsService
	Purchases     *PurchasesService
	Invoices      *InvoicesService
	Webhooks      *WebhooksService
	Paywalls      *PaywallsService
	Charts        *ChartsService
	Metrics       *MetricsService
	Audit         *AuditService
	Benchmarks    *BenchmarksService
	Apps          *AppsService
	// add as we go: Discounts, Experiments, VirtualCurrencies catalog
}

func NewClient(opts Options) *Client {
	base := opts.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	u, _ := url.Parse(base)
	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	ua := opts.UserAgent
	if ua == "" {
		ua = "revenuecat-cli/dev"
	}
	c := &Client{baseURL: u, apiKey: opts.APIKey, http: hc, userAgent: ua}
	c.Projects = &ProjectsService{c: c}
	c.Customers = &CustomersService{c: c}
	c.Entitlements = &EntitlementsService{c: c}
	c.Offerings = &OfferingsService{c: c}
	c.Packages = &PackagesService{c: c}
	c.Products = &ProductsService{c: c}
	c.Subscriptions = &SubscriptionsService{c: c}
	c.Purchases = &PurchasesService{c: c}
	c.Invoices = &InvoicesService{c: c}
	c.Webhooks = &WebhooksService{c: c}
	c.Paywalls = &PaywallsService{c: c}
	c.Charts = &ChartsService{c: c}
	c.Metrics = &MetricsService{c: c}
	c.Audit = &AuditService{c: c}
	c.Benchmarks = &BenchmarksService{c: c}
	c.Apps = &AppsService{c: c}
	return c
}

// do is the single chokepoint for all HTTP. Streaming (SSE) helpers live next
// to it but call into a different path that returns a *http.Response.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}

	urlStr, err := c.buildURL(path)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, urlStr, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return parseError(resp)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// stream opens a long-lived SSE-style connection. Used by future `rc events tail`
// and the planned chat experience. Caller owns closing the response body.
func (c *Client) stream(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	urlStr, err := c.buildURL(path)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, urlStr, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", c.userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		err := parseError(resp)
		resp.Body.Close()
		return nil, err
	}
	return resp, nil
}

// Page wraps cursor-paginated list responses.
//
// RevenueCat v2 uses Stripe-style envelopes: a full URL in `next_page` is the
// cursor for the next request, or null when exhausted. `object` is always
// "list"; `url` is the canonical URL of this page.
type Page[T any] struct {
	Items    []T    `json:"items"`
	NextPage string `json:"next_page,omitempty"`
	Object   string `json:"object,omitempty"`
	URL      string `json:"url,omitempty"`
}

// Millis is a unix-millisecond timestamp. The v2 API emits these as ints.
type Millis int64

// buildURL combines the client's base URL with a path that may include a
// query string. Query must go in URL.RawQuery, not concatenated into Path —
// otherwise the server receives a literal "?" in the path and 404s.
func (c *Client) buildURL(path string) (string, error) {
	u := *c.baseURL
	if i := strings.IndexByte(path, '?'); i >= 0 {
		u.Path = u.Path + path[:i]
		u.RawQuery = path[i+1:]
	} else {
		u.Path = u.Path + path
	}
	return u.String(), nil
}

func encodePath(parts ...string) string {
	out := ""
	for _, p := range parts {
		out += "/" + url.PathEscape(p)
	}
	return out
}

var _ = fmt.Sprintf
