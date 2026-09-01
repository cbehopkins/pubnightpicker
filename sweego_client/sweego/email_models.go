package sweego

type EmailAddress struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

type SendEmailRequest struct {
	Channel      string            `json:"channel"`
	From         EmailAddress      `json:"from"`
	Provider     string            `json:"provider"`
	Subject      string            `json:"subject"`
	Recipients   []EmailAddress    `json:"recipients"`
	MessageTxt   string            `json:"message-txt"`
	CampaignType string            `json:"campaign-type"`
	DryRun       bool              `json:"dry-run,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
}

type BulkRecipient struct {
	Email     string         `json:"email"`
	Name      string         `json:"name,omitempty"`
	Variables map[string]any `json:"variables,omitempty"`
}

type BulkEmailRequest struct {
	Channel      string            `json:"channel"`
	From         EmailAddress      `json:"from"`
	Provider     string            `json:"provider"`
	Subject      string            `json:"subject,omitempty"`
	Recipients   []BulkRecipient   `json:"recipients"`
	MessageTxt   string            `json:"message-txt,omitempty"`
	CampaignType string            `json:"campaign-type,omitempty"`
	TemplateID   string            `json:"template-id,omitempty"`
	DryRun       bool              `json:"dry-run,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
}
