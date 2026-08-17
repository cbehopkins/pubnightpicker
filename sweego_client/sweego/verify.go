package sweego

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// PubnightMessageIDHeader is the application-owned custom header used to
// correlate an outgoing email with its record in Sweego's logs.
const PubnightMessageIDHeader = "X-Pubnight-Message-ID"

// NewCorrelationID generates a unique, application-owned message ID to embed
// in the PubnightMessageIDHeader before a send attempt.
func NewCorrelationID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate correlation id: %w", err)
	}
	return "pn-" + hex.EncodeToString(buf), nil
}

// VerificationStatus distinguishes the possible outcomes of a log verification.
type VerificationStatus string

const (
	VerificationFound      VerificationStatus = "FOUND"
	VerificationNotFound   VerificationStatus = "NOT_FOUND"
	VerificationQueryError VerificationStatus = "QUERY_ERROR"
)

// VerificationResult is the structured outcome of VerifyMessage.
type VerificationResult struct {
	Status        VerificationStatus
	CorrelationID string
	Recipient     string
	TransactionID string
	SwgUID        string
	EmailStatus   string
	Err           error
}

// LogVerifier independently confirms, via Sweego's /logs/ endpoint, whether a
// message we attempted to send has a corresponding record in Sweego. It does
// not depend on having received a synchronous send response.
type LogVerifier struct {
	Client     *Client
	HeaderName string
	Tolerance  time.Duration
}

func NewLogVerifier(client *Client, tolerance time.Duration) *LogVerifier {
	return &LogVerifier{
		Client:     client,
		HeaderName: PubnightMessageIDHeader,
		Tolerance:  tolerance,
	}
}

// VerifyMessage queries Sweego's logs for messages sent to recipient around
// sentAt (+/- Tolerance) and looks for a candidate record whose
// PubnightMessageIDHeader value exactly matches correlationID.
//
// Sweego's logs date filters are day-granularity only, so the tolerance is
// used only to pick the covering day(s); no further local time filtering is
// performed. Candidate records are searched exhaustively so the correct
// message is found even among several sent to the same recipient.
func (v *LogVerifier) VerifyMessage(ctx context.Context, correlationID, recipient string, sentAt time.Time) VerificationResult {
	start := sentAt.Add(-v.Tolerance)
	end := sentAt.Add(v.Tolerance)

	req := LogsRequest{
		Channel:    "email",
		StartDate:  start.Format("2006-01-02"),
		EndDate:    end.Format("2006-01-02"),
		SearchWord: recipient,
		Size:       500,
	}

	status, body, err := v.Client.QueryLogs(ctx, req)
	if err != nil {
		return VerificationResult{Status: VerificationQueryError, CorrelationID: correlationID, Recipient: recipient, Err: err}
	}
	if status < 200 || status >= 300 {
		return VerificationResult{Status: VerificationQueryError, CorrelationID: correlationID, Recipient: recipient, Err: fmt.Errorf("logs query returned non-2xx status: %d", status)}
	}

	var logsResp LogsResponse
	if err := json.Unmarshal(body, &logsResp); err != nil {
		return VerificationResult{Status: VerificationQueryError, CorrelationID: correlationID, Recipient: recipient, Err: fmt.Errorf("decode logs response: %w", err)}
	}

	for _, record := range logsResp.Result {
		value, ok := headerValue(record.Headers, v.HeaderName)
		if !ok || value != correlationID {
			continue
		}
		return VerificationResult{
			Status:        VerificationFound,
			CorrelationID: correlationID,
			Recipient:     record.EmailTo,
			TransactionID: record.TransactionID,
			SwgUID:        record.SwgUID,
			EmailStatus:   record.Status,
		}
	}

	return VerificationResult{Status: VerificationNotFound, CorrelationID: correlationID, Recipient: recipient}
}

// headerValue looks up name in headers case-insensitively, returning its
// string value. Sweego normalises header names to lowercase in log records.
func headerValue(headers map[string]any, name string) (string, bool) {
	target := strings.ToLower(name)
	for k, raw := range headers {
		if strings.ToLower(k) != target {
			continue
		}
		value, ok := raw.(string)
		return value, ok
	}
	return "", false
}
