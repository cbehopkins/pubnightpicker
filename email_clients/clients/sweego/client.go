package sweego

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"email_clients/clients"
)

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

type HTTPResult = clients.HTTPResult

func NewClient(baseURL, token string, timeout time.Duration) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// Do sends an authenticated request with payload encoded as JSON. A nil payload
// sends no request body. The raw status, headers, and response body are retained.
func (c *Client) Do(ctx context.Context, method, path string, payload any) (HTTPResult, error) {
	var reader io.Reader
	if payload != nil {
		body, err := json.Marshal(payload)
		if err != nil {
			return HTTPResult{}, fmt.Errorf("marshal request: %w", err)
		}
		reader = bytes.NewReader(body)
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return HTTPResult{}, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Api-Key", c.token)
	httpReq.Header.Set("Accept", "application/json")
	if payload != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return HTTPResult{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return HTTPResult{Status: resp.StatusCode, Headers: resp.Header.Clone()}, fmt.Errorf("read response body: %w", err)
	}

	return HTTPResult{Status: resp.StatusCode, Headers: resp.Header.Clone(), Body: respBody}, nil
}
