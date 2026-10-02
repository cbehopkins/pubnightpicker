package mailtrap

import (
	"errors"

	sdk "github.com/mailtrap/mailtrap-go"
)

const MaxBatchRecipients = 500
const CorrelationVariable = "correlation_id"

type Client struct {
	sdk         *sdk.Client
	sendOptions SendOptions
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
