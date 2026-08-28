package sweego

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

type HTTPResult struct {
	Status  int
	Headers http.Header
	Body    []byte
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

func (c *Client) SendEmail(ctx context.Context, req SendEmailRequest) (HTTPResult, error) {
	return c.postJSONWithHeaders(ctx, "/send", req)
}

func (c *Client) SendBulkEmail(ctx context.Context, req BulkEmailRequest) (HTTPResult, error) {
	return c.postJSONWithHeaders(ctx, "/send/bulk/email", req)
}

// CreateTemplate posts a new email template. Sweego scopes templates by client
// and channel, and both create and update are POST.
func (c *Client) CreateTemplate(ctx context.Context, clientUUID string, req CreateTemplateRequest) (HTTPResult, error) {
	return c.postJSONWithHeaders(ctx, templatesPath(clientUUID), req)
}

func (c *Client) UpdateTemplate(ctx context.Context, clientUUID, templateUUID string, req UpdateTemplateRequest) (HTTPResult, error) {
	return c.postJSONWithHeaders(ctx, templatePath(clientUUID, templateUUID), req)
}

// GetTemplate reads a stored template back, which is how the client verifies
// that Sweego kept the uploaded content unchanged.
func (c *Client) GetTemplate(ctx context.Context, clientUUID, templateUUID string) (HTTPResult, error) {
	return c.requestWithHeaders(ctx, http.MethodGet, templatePath(clientUUID, templateUUID), nil)
}

// DeleteTemplate removes a template. Sweego caps the number of stored templates
// per plan, so experiments need to clean up after themselves.
func (c *Client) DeleteTemplate(ctx context.Context, clientUUID, templateUUID string) (HTTPResult, error) {
	return c.requestWithHeaders(ctx, http.MethodDelete, templatePath(clientUUID, templateUUID), nil)
}

func templatePath(clientUUID, templateUUID string) string {
	return templatesPath(clientUUID) + "/" + url.PathEscape(templateUUID)
}

func templatesPath(clientUUID string) string {
	return "/clients/" + url.PathEscape(clientUUID) + "/channels/email/templates"
}

// TemplateUUID scans a template API response for the created or updated
// template identifier. The accepted key names are probes rather than a claim
// about the provider contract.
func TemplateUUID(body []byte) (string, error) {
	var root any
	if err := json.Unmarshal(body, &root); err != nil {
		return "", fmt.Errorf("decode template response JSON: %w", err)
	}
	if uuid := findTemplateUUID(root); uuid != "" {
		return uuid, nil
	}
	return "", errors.New("no template uuid found in response")
}

func findTemplateUUID(value any) string {
	switch current := value.(type) {
	case map[string]any:
		for _, key := range []string{"uuid", "uuid_template", "template_uuid", "id"} {
			if text, ok := current[key].(string); ok && text != "" {
				return text
			}
		}
		for _, child := range current {
			if uuid := findTemplateUUID(child); uuid != "" {
				return uuid
			}
		}
	case []any:
		for _, child := range current {
			if uuid := findTemplateUUID(child); uuid != "" {
				return uuid
			}
		}
	}
	return ""
}

func (c *Client) QueryLogs(ctx context.Context, req LogsRequest) (int, []byte, error) {
	return c.postJSON(ctx, "/logs/", req)
}

func (c *Client) postJSON(ctx context.Context, path string, payload any) (int, []byte, error) {
	result, err := c.postJSONWithHeaders(ctx, path, payload)
	return result.Status, result.Body, err
}

func (c *Client) postJSONWithHeaders(ctx context.Context, path string, payload any) (HTTPResult, error) {
	return c.requestWithHeaders(ctx, http.MethodPost, path, payload)
}

// requestWithHeaders sends payload as JSON, or no body at all when payload is nil.
func (c *Client) requestWithHeaders(ctx context.Context, method, path string, payload any) (HTTPResult, error) {
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
