package sweego

import (
	"context"
	"net/http"
)

func (c *Client) SendEmail(ctx context.Context, req SendEmailRequest) (HTTPResult, error) {
	return c.Do(ctx, http.MethodPost, "/send", req)
}

func (c *Client) SendBulkEmail(ctx context.Context, req BulkEmailRequest) (HTTPResult, error) {
	return c.Do(ctx, http.MethodPost, "/send/bulk/email", req)
}
