package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"email_clients/clients"
	"email_clients/clients/sweego"
)

type Verifier struct {
	Client     *Client
	HeaderName string
	Tolerance  time.Duration
}

var _ clients.EmailVerifier = (*Verifier)(nil)

func NewVerifier(client *Client, tolerance time.Duration) *Verifier {
	return &Verifier{
		Client:     client,
		HeaderName: sweego.PubnightMessageIDHeader,
		Tolerance:  tolerance,
	}
}

// Verify uses tolerance only to select covering dates because Sweego's
// logs API does not support time-of-day filters.
func (v *Verifier) Verify(ctx context.Context, request clients.VerifyRequest) (clients.VerifyResult, error) {
	start := request.SentAt.Add(-v.Tolerance)
	end := request.SentAt.Add(v.Tolerance)
	result, err := v.Client.Query(ctx, Request{
		Channel:    "email",
		StartDate:  start.Format("2006-01-02"),
		EndDate:    end.Format("2006-01-02"),
		SearchWord: request.Recipient,
		Size:       500,
	})
	if err != nil {
		return clients.VerifyResult{}, err
	}
	if result.Status < 200 || result.Status >= 300 {
		return clients.VerifyResult{}, fmt.Errorf("logs query returned non-2xx status: %d", result.Status)
	}

	var response Response
	if err := json.Unmarshal(result.Body, &response); err != nil {
		return clients.VerifyResult{}, fmt.Errorf("decode logs response: %w", err)
	}
	for _, record := range response.Result {
		value, ok := HeaderValue(record.Headers, v.HeaderName)
		if !ok || value != request.CorrelationID {
			continue
		}
		return clients.VerifyResult{Found: true, PMUID: record.SwgUID}, nil
	}
	return clients.VerifyResult{}, nil
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
