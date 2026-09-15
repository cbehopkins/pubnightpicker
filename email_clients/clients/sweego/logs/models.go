package logs

// Request is the request body for POST /logs/ (channel=email variant).
// Sweego's date filters are day-granularity only (YYYY-MM-DD), there is no
// documented way to filter by time-of-day.
type Request struct {
	Channel    string `json:"channel"`
	StartDate  string `json:"start_date,omitempty"`
	EndDate    string `json:"end_date,omitempty"`
	SearchWord string `json:"search_word,omitempty"`
	Offset     int    `json:"offset,omitempty"`
	Size       int    `json:"size,omitempty"`
}

// Record is a single message record as returned in Response.Result for the
// email channel. The field set was observed from a live Sweego response.
type Record struct {
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

// Response is the response body for POST /logs/.
type Response struct {
	NbPage                int      `json:"nb_page"`
	NbResult              int      `json:"nb_result"`
	NbResultWithoutOffset int      `json:"nb_result_without_offset"`
	Result                []Record `json:"result"`
	State                 bool     `json:"state"`
	Error                 []string `json:"error,omitempty"`
	Msg                   string   `json:"msg,omitempty"`
}
