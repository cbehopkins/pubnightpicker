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
