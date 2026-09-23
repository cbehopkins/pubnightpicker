package clients

import (
	"context"
	"time"
)

type Address struct {
	Email string
	Name  string
}

type Recipient struct {
	Address
	Variables map[string]any
}

type Email struct {
	From       Address
	To         []Recipient
	Subject    string
	TemplateID string
	Text       string
	Variables  map[string]any
	Headers    map[string]string
}

type SendResult struct {
	Recipients []RecipientResult
}

type RecipientResult struct {
	PMUID string
}

type EmailClient interface {
	Send(context.Context, Email) (SendResult, error)
}

type VerifyRequest struct {
	CorrelationID string
	Recipient     string
	SentAt        time.Time
}

type VerifyResult struct {
	Found bool
	PMUID string
}

type EmailVerifier interface {
	Verify(context.Context, VerifyRequest) (VerifyResult, error)
}
