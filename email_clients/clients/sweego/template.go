package sweego

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

func (c *Client) CreateTemplate(ctx context.Context, clientUUID string, req CreateTemplateRequest) (HTTPResult, error) {
	return c.Do(ctx, http.MethodPost, templatesPath(clientUUID), req)
}

func (c *Client) UpdateTemplate(ctx context.Context, clientUUID, templateUUID string, req UpdateTemplateRequest) (HTTPResult, error) {
	return c.Do(ctx, http.MethodPost, templatePath(clientUUID, templateUUID), req)
}

func (c *Client) GetTemplate(ctx context.Context, clientUUID, templateUUID string) (HTTPResult, error) {
	return c.Do(ctx, http.MethodGet, templatePath(clientUUID, templateUUID), nil)
}

func (c *Client) DeleteTemplate(ctx context.Context, clientUUID, templateUUID string) (HTTPResult, error) {
	return c.Do(ctx, http.MethodDelete, templatePath(clientUUID, templateUUID), nil)
}

func templatePath(clientUUID, templateUUID string) string {
	return templatesPath(clientUUID) + "/" + url.PathEscape(templateUUID)
}

func templatesPath(clientUUID string) string {
	return "/clients/" + url.PathEscape(clientUUID) + "/channels/email/templates"
}

func TemplateUUID(body []byte) (string, error) {
	var response struct {
		UUID string `json:"uuid"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("decode template response JSON: %w", err)
	}
	if response.UUID == "" {
		return "", errors.New("no template uuid found in response")
	}
	return response.UUID, nil
}
