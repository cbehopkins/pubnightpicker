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
	Headers      map[string]string `json:"headers,omitempty"`
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
