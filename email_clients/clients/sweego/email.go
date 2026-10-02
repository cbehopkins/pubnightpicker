package sweego

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"email_clients/clients"
)

var _ clients.EmailClient = (*Client)(nil)

var ErrNoRecipients = errors.New("email has no recipients")

func (c *Client) Send(ctx context.Context, email clients.Email) (clients.SendResult, error) {
	if len(email.To) == 0 {
		return clients.SendResult{}, ErrNoRecipients
	}

	path := "/send"
	var request any = c.singleRequest(email)
	if requiresBulkEndpoint(email) {
		path = "/send/bulk/email"
		request = c.bulkRequest(email)
	}

	response, err := c.Do(ctx, http.MethodPost, path, request)
	if err != nil {
		return clients.SendResult{}, err
	}
	if response.Status < http.StatusOK || response.Status >= http.StatusMultipleChoices {
		return clients.SendResult{}, fmt.Errorf("sweego send returned HTTP %d: %s", response.Status, strings.TrimSpace(string(response.Body)))
	}

	pmuids, err := parsePMUIDs(response.Body)
	if err != nil {
		return clients.SendResult{}, err
	}

	result := clients.SendResult{Recipients: make([]clients.RecipientResult, len(email.To))}
	for index, recipient := range email.To {
		pmuid := pmuids[recipient.Email]
		if pmuid == "" && len(email.To) == 1 && len(pmuids) == 1 {
			for _, value := range pmuids {
				pmuid = value
			}
		}
		if pmuid == "" {
			return clients.SendResult{}, fmt.Errorf("sweego response has no swg_uid for recipient %q", recipient.Email)
		}
		result.Recipients[index].PMUID = pmuid
	}
	return result, nil
}

func requiresBulkEndpoint(email clients.Email) bool {
	if len(email.To) != 1 || email.TemplateID != "" || len(email.Variables) != 0 {
		return true
	}
	return len(email.To[0].Variables) != 0
}

func (c *Client) singleRequest(email clients.Email) sendEmailRequest {
	return sendEmailRequest{
		Channel:      "email",
		From:         newEmailAddress(email.From),
		Provider:     c.sendOptions.Provider,
		Subject:      email.Subject,
		Recipients:   []emailAddress{newEmailAddress(email.To[0].Address)},
		MessageTxt:   email.Text,
		CampaignType: c.sendOptions.CampaignType,
		DryRun:       c.sendOptions.DryRun,
		Headers:      email.Headers,
	}
}

func (c *Client) bulkRequest(email clients.Email) bulkEmailRequest {
	recipients := make([]bulkRecipient, len(email.To))
	for index, recipient := range email.To {
		recipients[index] = bulkRecipient{
			Email:     recipient.Email,
			Name:      recipient.Name,
			Variables: mergedVariables(email.Variables, recipient.Variables),
		}
	}
	return bulkEmailRequest{
		Channel:      "email",
		From:         newEmailAddress(email.From),
		Provider:     c.sendOptions.Provider,
		Subject:      email.Subject,
		Recipients:   recipients,
		MessageTxt:   email.Text,
		CampaignType: c.sendOptions.CampaignType,
		TemplateID:   email.TemplateID,
		DryRun:       c.sendOptions.DryRun,
		Headers:      email.Headers,
	}
}

func parsePMUIDs(body []byte) (map[string]string, error) {
	var root any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("decode Sweego send response: %w", err)
	}
	pmuids := make(map[string]string)
	collectPMUIDs(root, pmuids)
	if len(pmuids) == 0 {
		return nil, errors.New("sweego send response contains no swg_uid")
	}
	return pmuids, nil
}

func collectPMUIDs(value any, pmuids map[string]string) {
	switch current := value.(type) {
	case map[string]any:
		var recipient, pmuid string
		for key, child := range current {
			normalized := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
			switch normalized {
			case "swg_uids", "swguids":
				if identifiers, ok := child.(map[string]any); ok {
					for email, identifier := range identifiers {
						if text, ok := identifier.(string); ok {
							pmuids[email] = text
						}
					}
				}
			case "recipient", "email", "to":
				recipient, _ = child.(string)
			case "swg_uid", "swguid":
				pmuid, _ = child.(string)
			}
			collectPMUIDs(child, pmuids)
		}
		if recipient != "" && pmuid != "" {
			pmuids[recipient] = pmuid
		}
	case []any:
		for _, child := range current {
			collectPMUIDs(child, pmuids)
		}
	}
}
