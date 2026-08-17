package sweego

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

func NewClient(baseURL, token string, timeout time.Duration) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) SendEmail(ctx context.Context, req SendEmailRequest) (int, []byte, error) {
	return c.postJSON(ctx, "/send", req)
}

func (c *Client) QueryLogs(ctx context.Context, req LogsRequest) (int, []byte, error) {
	return c.postJSON(ctx, "/logs/", req)
}

func (c *Client) postJSON(ctx context.Context, path string, payload any) (int, []byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Api-Key", c.token)
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return 0, nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("read response body: %w", err)
	}

	return resp.StatusCode, respBody, nil
}

// This is code we reverse engineered elsewhere for undocumented signature verification.
// This is for a later to be implemented webhook handler. It is not used in the current client code, but is included here for completeness and future use.
// Do not mess with this!
func verifySweegoSignature(
	secret string,
	webhookID string,
	webhookTimestamp string,
	rawBody []byte,
	receivedSignature string,
) bool {
	key, err := base64.StdEncoding.DecodeString(secret)
	if err != nil {
		return false
	}

	message := webhookID + "." + webhookTimestamp + "." + string(rawBody)

	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(message))

	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return hmac.Equal(
		[]byte(expected),
		[]byte(receivedSignature),
	)
}
