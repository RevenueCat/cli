package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const DefaultURL = "https://mcp.revenuecat.ai/mcp?surface=full"

type Client struct {
	url        string
	token      string
	userAgent  string
	httpClient *http.Client
}

type Options struct {
	URL        string
	Token      string
	UserAgent  string
	HTTPClient *http.Client
}

type Content struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

type ToolResult struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

type rpcRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      int            `json:"id"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func NewClient(opts Options) *Client {
	url := opts.URL
	if url == "" {
		url = DefaultURL
	}
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		url:        url,
		token:      opts.Token,
		userAgent:  opts.UserAgent,
		httpClient: httpClient,
	}
}

func (c *Client) CallTool(ctx context.Context, name string, arguments map[string]any) (*ToolResult, error) {
	body, err := json.Marshal(rpcRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params: map[string]any{
			"name":      name,
			"arguments": arguments,
		},
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling RevenueCat MCP: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 25<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("RevenueCat MCP returned HTTP %d: %s", resp.StatusCode, safeResponseMessage(responseBody))
	}

	payload, err := decodeRPCPayload(resp.Header.Get("Content-Type"), responseBody)
	if err != nil {
		return nil, err
	}
	var rpcResp rpcResponse
	if err := json.Unmarshal(payload, &rpcResp); err != nil {
		return nil, fmt.Errorf("decoding RevenueCat MCP response: %w", err)
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("RevenueCat MCP error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	var result ToolResult
	if err := json.Unmarshal(rpcResp.Result, &result); err != nil {
		return nil, fmt.Errorf("decoding RevenueCat MCP tool result: %w", err)
	}
	if result.IsError {
		return nil, fmt.Errorf("Paywall AI Editor: %s", result.Text())
	}
	return &result, nil
}

func (r *ToolResult) Text() string {
	var parts []string
	for _, item := range r.Content {
		if item.Type == "text" && item.Text != "" {
			parts = append(parts, item.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func (r *ToolResult) ImageCount() int {
	count := 0
	for _, item := range r.Content {
		if item.Type == "image" {
			count++
		}
	}
	return count
}

func decodeRPCPayload(contentType string, body []byte) ([]byte, error) {
	if !strings.Contains(strings.ToLower(contentType), "text/event-stream") {
		return body, nil
	}
	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			return bytes.TrimSpace([]byte(strings.TrimPrefix(line, "data:"))), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("RevenueCat MCP returned an empty event stream")
}

func safeResponseMessage(body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if json.Valid(body) && len(trimmed) <= 1000 {
		return trimmed
	}
	return "non-JSON response"
}
