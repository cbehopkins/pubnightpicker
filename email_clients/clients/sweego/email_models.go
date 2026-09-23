package sweego

import "email_clients/clients"

type emailAddress struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

type sendEmailRequest struct {
	Channel      string            `json:"channel"`
	From         emailAddress      `json:"from"`
	Provider     string            `json:"provider"`
	Subject      string            `json:"subject"`
	Recipients   []emailAddress    `json:"recipients"`
	MessageTxt   string            `json:"message-txt"`
	CampaignType string            `json:"campaign-type"`
	DryRun       bool              `json:"dry-run,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
}

type bulkRecipient struct {
	Email     string         `json:"email"`
	Name      string         `json:"name,omitempty"`
	Variables map[string]any `json:"variables,omitempty"`
}

type bulkEmailRequest struct {
	Channel      string            `json:"channel"`
	From         emailAddress      `json:"from"`
	Provider     string            `json:"provider"`
	Subject      string            `json:"subject,omitempty"`
	Recipients   []bulkRecipient   `json:"recipients"`
	MessageTxt   string            `json:"message-txt,omitempty"`
	CampaignType string            `json:"campaign-type,omitempty"`
	TemplateID   string            `json:"template-id,omitempty"`
	DryRun       bool              `json:"dry-run,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
}

func newEmailAddress(address clients.Address) emailAddress {
	return emailAddress{Email: address.Email, Name: address.Name}
}

func mergedVariables(common, recipient map[string]any) map[string]any {
	if len(common) == 0 && len(recipient) == 0 {
		return nil
	}

	variables := make(map[string]any, len(common)+len(recipient))
	for name, value := range common {
		variables[name] = value
	}
	for name, value := range recipient {
		variables[name] = value
	}
	return variables
}
