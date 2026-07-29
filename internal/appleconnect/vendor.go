package appleconnect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
)

var sapVendorNumberPattern = regexp.MustCompile(`"sapVendorNumber"\s*:\s*"?(\d+)`)

// FetchVendorNumber returns the provider's sales-report vendor number using
// the internal endpoint behind the App Store Connect payments page (the same
// place the docs tell users to copy it from manually). Best-effort: callers
// should fall back to asking the user when this errors.
func (c *Client) FetchVendorNumber(ctx context.Context, session *Session) (string, error) {
	if session == nil || session.client != c {
		return "", errors.New("authenticated Apple session is required")
	}
	headers := http.Header{
		"Accept":           []string{"application/json"},
		"Origin":           []string{c.ascBaseURL},
		"Referer":          []string{c.ascBaseURL + "/itc/payments_and_financial_reports"},
		"X-Requested-With": []string{"olympus-ui"},
	}
	endpoint := fmt.Sprintf(
		"%s/WebObjects/iTunesConnect.woa/ra/paymentConsolidation/providers/%d/sapVendorNumbers",
		c.ascBaseURL, session.Provider.ID,
	)
	// The response shape is undocumented; extract the field from the raw JSON
	// rather than committing to a wrapper structure.
	var raw json.RawMessage
	if err := c.doJSON(ctx, http.MethodGet, endpoint, nil, &raw, headers); err != nil {
		return "", fmt.Errorf("fetch vendor number: %w", err)
	}
	match := sapVendorNumberPattern.FindSubmatch(raw)
	if match == nil {
		return "", errors.New("the App Store Connect response did not include a vendor number")
	}
	return string(match[1]), nil
}
