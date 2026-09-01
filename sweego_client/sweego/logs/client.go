package logs

import (
	"context"
	"net/http"

	"sweego_client/sweego"
)

type Client struct {
	client *sweego.Client
}

func NewClient(client *sweego.Client) *Client {
	return &Client{client: client}
}

func (c *Client) Query(ctx context.Context, req Request) (sweego.HTTPResult, error) {
	return c.client.Do(ctx, http.MethodPost, "/logs/", req)
}
