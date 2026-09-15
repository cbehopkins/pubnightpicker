package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"email_clients/clients/sweego"
)

type VerificationStatus string

const (
	VerificationFound      VerificationStatus = "FOUND"
	VerificationNotFound   VerificationStatus = "NOT_FOUND"
	VerificationQueryError VerificationStatus = "QUERY_ERROR"
)

type VerificationResult struct {
	Status        VerificationStatus
	CorrelationID string
	Recipient     string
	TransactionID string
	SwgUID        string
	EmailStatus   string
	Err           error
}

type Verifier struct {
	Client     *Client
	HeaderName string
	Tolerance  time.Duration
}

func NewVerifier(client *Client, tolerance time.Duration) *Verifier {
	return &Verifier{
		Client:     client,
		HeaderName: sweego.PubnightMessageIDHeader,
		Tolerance:  tolerance,
	}
}

// VerifyMessage uses tolerance only to select covering dates because Sweego's
// logs API does not support time-of-day filters.
func (v *Verifier) VerifyMessage(ctx context.Context, correlationID, recipient string, sentAt time.Time) VerificationResult {
	start := sentAt.Add(-v.Tolerance)
	end := sentAt.Add(v.Tolerance)
	result, err := v.Client.Query(ctx, Request{
		Channel:    "email",
		StartDate:  start.Format("2006-01-02"),
		EndDate:    end.Format("2006-01-02"),
		SearchWord: recipient,
		Size:       500,
	})
	if err != nil {
		return VerificationResult{Status: VerificationQueryError, CorrelationID: correlationID, Recipient: recipient, Err: err}
	}
	if result.Status < 200 || result.Status >= 300 {
		return VerificationResult{Status: VerificationQueryError, CorrelationID: correlationID, Recipient: recipient, Err: fmt.Errorf("logs query returned non-2xx status: %d", result.Status)}
	}

	var response Response
	if err := json.Unmarshal(result.Body, &response); err != nil {
		return VerificationResult{Status: VerificationQueryError, CorrelationID: correlationID, Recipient: recipient, Err: fmt.Errorf("decode logs response: %w", err)}
	}
	for _, record := range response.Result {
		value, ok := HeaderValue(record.Headers, v.HeaderName)
		if !ok || value != correlationID {
			continue
		}
		return VerificationResult{
			Status: VerificationFound, CorrelationID: correlationID, Recipient: record.EmailTo,
			TransactionID: record.TransactionID, SwgUID: record.SwgUID, EmailStatus: record.Status,
		}
	}
	return VerificationResult{Status: VerificationNotFound, CorrelationID: correlationID, Recipient: recipient}
}

func HeaderValue(headers map[string]any, name string) (string, bool) {
	for key, raw := range headers {
		if !strings.EqualFold(key, name) {
			continue
		}
		value, ok := raw.(string)
		return value, ok
	}
	return "", false
}
