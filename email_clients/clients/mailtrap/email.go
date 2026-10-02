package mailtrap

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/mail"
	"slices"
	"strings"

	"email_clients/clients"
	"email_clients/internal/texttemplate"

	sdk "github.com/mailtrap/mailtrap-go"
)

var _ clients.EmailClient = (*Client)(nil)

func (c *Client) Send(ctx context.Context, email clients.Email) (clients.SendResult, error) {
	if err := ctx.Err(); err != nil {
		return clients.SendResult{}, err
	}
	if c == nil || c.sdk == nil {
		return clients.SendResult{}, errors.New("mailtrap client is not configured")
	}
	requests, err := c.requests(ctx, email)
	if err != nil {
		return clients.SendResult{}, err
	}
	if len(requests) == 1 {
		response, _, err := c.sdk.Send(ctx, &requests[0])
		if err != nil {
			return clients.SendResult{}, fmt.Errorf("mailtrap send: %w", err)
		}
		if !response.Success || !validMessageID(response.MessageIDs) {
			return clients.SendResult{}, fmt.Errorf("%w: single send must report success and exactly one message ID", ErrInvalidResponse)
		}
		return clients.SendResult{Recipients: []clients.RecipientResult{{PMUID: response.MessageIDs[0]}}}, nil
	}
	response, _, err := c.sdk.SendBatch(ctx, &sdk.BatchSendRequest{Requests: requests})
	if err != nil {
		return clients.SendResult{}, fmt.Errorf("mailtrap batch send: %w", err)
	}
	return batchResult(email.To, response)
}

func (c *Client) requests(ctx context.Context, email clients.Email) ([]sdk.SendRequest, error) {
	if len(email.To) == 0 {
		return nil, ErrNoRecipients
	}
	if len(email.To) > MaxBatchRecipients {
		return nil, fmt.Errorf("mailtrap supports at most %d recipients per Send", MaxBatchRecipients)
	}
	if err := validateAddress(email.From.Email); err != nil {
		return nil, fmt.Errorf("invalid sender: %w", err)
	}
	if email.TemplateID != "" {
		if strings.TrimSpace(email.TemplateID) == "" || email.Subject != "" || email.Text != "" {
			return nil, errors.New("mailtrap hosted templates require a non-empty TemplateID and empty Subject and Text")
		}
	} else if strings.TrimSpace(email.Subject) == "" || strings.TrimSpace(email.Text) == "" {
		return nil, errors.New("mailtrap raw sends require Subject and Text")
	}
	correlationID, err := correlationHeader(email.Headers)
	if err != nil {
		return nil, err
	}
	requests := make([]sdk.SendRequest, len(email.To))
	for index, recipient := range email.To {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := validateAddress(recipient.Email); err != nil {
			return nil, fmt.Errorf("invalid recipient %d: %w", index, err)
		}
		variables := maps.Clone(email.Variables)
		if variables == nil && len(recipient.Variables) != 0 {
			variables = make(map[string]any, len(recipient.Variables))
		}
		maps.Copy(variables, recipient.Variables)
		request := sdk.SendRequest{
			From:     sdk.Address{Email: email.From.Email, Name: email.From.Name},
			To:       []sdk.Address{{Email: recipient.Email, Name: recipient.Name}},
			Category: c.sendOptions.Category,
			Headers:  maps.Clone(email.Headers),
		}
		if correlationID != "" {
			request.CustomVariables = map[string]any{CorrelationVariable: correlationID}
		}
		if email.TemplateID != "" {
			request.TemplateUUID = email.TemplateID
			request.TemplateVariables = variables
		} else {
			request.Subject, err = texttemplate.Render(email.Subject, variables)
			if err == nil {
				request.Text, err = texttemplate.Render(email.Text, variables)
			}
			if err != nil {
				return nil, fmt.Errorf("render email for recipient %d %q: %w", index, recipient.Email, err)
			}
			if strings.TrimSpace(request.Subject) == "" || strings.TrimSpace(request.Text) == "" {
				return nil, fmt.Errorf("rendered Subject and Text are required for recipient %d %q", index, recipient.Email)
			}
		}
		requests[index] = request
	}
	return requests, nil
}

func validateAddress(address string) error {
	parsed, err := mail.ParseAddress(address)
	if err != nil || parsed.Address != address {
		return fmt.Errorf("%q must be a bare email address", address)
	}
	return nil
}

func correlationHeader(headers map[string]string) (string, error) {
	var value string
	for name, current := range headers {
		if !strings.EqualFold(name, clients.CorrelationHeader) {
			continue
		}
		if strings.TrimSpace(current) == "" {
			return "", errors.New("mailtrap correlation header must not be empty")
		}
		if value != "" && value != current {
			return "", errors.New("mailtrap correlation headers contain conflicting values")
		}
		value = current
	}
	return value, nil
}

func validMessageID(ids []string) bool {
	return len(ids) == 1 && strings.TrimSpace(ids[0]) != ""
}

func batchResult(recipients []clients.Recipient, response *sdk.BatchSendResponse) (clients.SendResult, error) {
	if len(response.Responses) != len(recipients) {
		return clients.SendResult{}, fmt.Errorf("%w: got %d batch results for %d recipients", ErrInvalidResponse, len(response.Responses), len(recipients))
	}
	result := clients.SendResult{Recipients: make([]clients.RecipientResult, len(recipients))}
	batchErr := &BatchError{Messages: slices.Clone(response.Errors)}
	if !response.Success && len(batchErr.Messages) == 0 {
		batchErr.Messages = []string{"batch response did not report overall success"}
	}
	for index, item := range response.Responses {
		diagnostic := RecipientError{Index: index, Recipient: recipients[index].Email, Messages: slices.Clone(item.Errors)}
		switch {
		case item.Success && validMessageID(item.MessageIDs):
			result.Recipients[index].PMUID = item.MessageIDs[0]
			if len(item.Errors) != 0 {
				diagnostic.Messages = append(diagnostic.Messages, "successful item also reported errors")
				batchErr.InvalidResults = append(batchErr.InvalidResults, diagnostic)
			}
		case !item.Success && len(item.Errors) != 0 && len(item.MessageIDs) == 0:
			batchErr.Refusals = append(batchErr.Refusals, diagnostic)
		default:
			diagnostic.Messages = append(diagnostic.Messages, "expected success with one message ID, or refusal with errors and no IDs")
			batchErr.InvalidResults = append(batchErr.InvalidResults, diagnostic)
		}
	}
	if len(batchErr.Messages)+len(batchErr.Refusals)+len(batchErr.InvalidResults) != 0 {
		return result, batchErr
	}
	return result, nil
}
