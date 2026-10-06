package mailtrap

import (
	"errors"

	"email_clients/clients"

	sdk "github.com/mailtrap/mailtrap-go"
)

const MaxBatchRecipients = 500
const CorrelationVariable = "correlation_id"

var ErrSandboxNotConfigured = errors.New("mailtrap sandbox client is not configured")

type Client struct {
	sdk            *sdk.Client
	sandboxSDK     *sdk.Client
	sendOptions    SendOptions
	dryRunCallback clients.DryRunCallback
}

type SendOptions struct {
	Category string
}

func NewClient(client *sdk.Client) (*Client, error) {
	if client == nil {
		return nil, errors.New("mailtrap SDK client is nil")
	}
	return &Client{sdk: client}, nil
}

func (c *Client) WithSendOptions(options SendOptions) *Client {
	clone := *c
	clone.sendOptions = options
	return &clone
}

func (c *Client) SetDryRunCallback(callback clients.DryRunCallback) {
	c.dryRunCallback = callback
}

func (c *Client) WithSandboxClient(client *sdk.Client) (*Client, error) {
	if client == nil {
		return nil, ErrSandboxNotConfigured
	}
	clone := *c
	clone.sandboxSDK = client
	return &clone, nil
}
