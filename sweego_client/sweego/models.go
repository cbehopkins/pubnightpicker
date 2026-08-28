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

// BulkRecipient is a single entry of the bulk endpoint's recipients array.
// Variables carries the per-recipient template substitution data.
type BulkRecipient struct {
	Email     string         `json:"email"`
	Name      string         `json:"name,omitempty"`
	Variables map[string]any `json:"variables,omitempty"`
}

// BulkEmailRequest is the request body used by Sweego's bulk email endpoint.
// Field names follow Sweego's documented request sample, which hyphenates
// template-id and dry-run rather than using snake case.
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

// CreateTemplateRequest is the body for POST
// /clients/{client}/channels/email/templates. Template holds the template
// source verbatim; Sweego exposes no separate html and text fields.
type CreateTemplateRequest struct {
	Name     string `json:"name"`
	Template string `json:"template"`
}

// UpdateTemplateRequest is the body for POST
// /clients/{client}/channels/email/templates/{template}. Sweego's update
// sample includes template_type where the create sample does not.
type UpdateTemplateRequest struct {
	Name         string `json:"name"`
	Template     string `json:"template"`
	TemplateType string `json:"template_type"`
}

// LogsRequest is the request body for POST /logs/ (channel=email variant).
// Sweego's date filters are day-granularity only (YYYY-MM-DD), there is no
// documented way to filter by time-of-day.
type LogsRequest struct {
	Channel    string `json:"channel"`
	StartDate  string `json:"start_date,omitempty"`
	EndDate    string `json:"end_date,omitempty"`
	SearchWord string `json:"search_word,omitempty"`
	Offset     int    `json:"offset,omitempty"`
	Size       int    `json:"size,omitempty"`
}

// LogRecord is a single message record as returned in LogsResponse.Result
// for the email channel. Field set observed from a live Sweego response.
type LogRecord struct {
	Channel         string         `json:"channel"`
	Status          string         `json:"status"`
	CampaignID      string         `json:"campaign_id"`
	DomainFrom      string         `json:"domain_from"`
	DomainTo        string         `json:"domain_to"`
	DryRun          bool           `json:"dry_run"`
	EmailCreation   string         `json:"email_creation"`
	EmailFrom       string         `json:"email_from"`
	EmailLastUpdate string         `json:"email_last_update"`
	EmailState      string         `json:"email_state"`
	EmailTo         string         `json:"email_to"`
	Headers         map[string]any `json:"headers"`
	Subject         string         `json:"subject"`
	SwgUID          string         `json:"swg_uid"`
	TransactionID   string         `json:"transaction_id"`
}

// LogsResponse is the response body for POST /logs/.
type LogsResponse struct {
	NbPage                int         `json:"nb_page"`
	NbResult              int         `json:"nb_result"`
	NbResultWithoutOffset int         `json:"nb_result_without_offset"`
	Result                []LogRecord `json:"result"`
	State                 bool        `json:"state"`
	Error                 []string    `json:"error,omitempty"`
	Msg                   string      `json:"msg,omitempty"`
}
