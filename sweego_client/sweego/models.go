package sweego

type EmailAddress struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

type SendEmailRequest struct {
	Channel      string         `json:"channel"`
	From         EmailAddress   `json:"from"`
	Provider     string         `json:"provider"`
	Subject      string         `json:"subject"`
	Recipients   []EmailAddress `json:"recipients"`
	MessageTxt   string         `json:"message-txt"`
	CampaignType string         `json:"campaign-type"`
}

type LogsRequest struct {
	SearchWord string `json:"search_word,omitempty"`
	To         string `json:"to,omitempty"`
	Date       string `json:"date,omitempty"`
}
