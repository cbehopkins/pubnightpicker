package logs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"sweego_client/sweego"
)

type RecoveryOperation struct {
	TransactionID string
	SubmittedAt   time.Time
	Sender        string
	Recipients    []string
}

type RecoveryOptions struct {
	Tolerance  time.Duration
	RetryDelay time.Duration
	Attempts   int
}

type RecoveryStatus string

const (
	Recovered  RecoveryStatus = "RECOVERED"
	Unresolved RecoveryStatus = "UNRESOLVED"
	Ambiguous  RecoveryStatus = "AMBIGUOUS"
)

type RecoveryResult struct {
	Recipient  string
	Status     RecoveryStatus
	SwgUID     string
	Record     *Record
	Candidates []Record
	Reason     string
}

type QueryObservation struct {
	Attempt   int
	Recipient string
	Response  sweego.HTTPResult
	Err       error
}

func Recover(ctx context.Context, client *Client, operation RecoveryOperation, correlationID string, options RecoveryOptions) ([]RecoveryResult, []QueryObservation, error) {
	results := make([]RecoveryResult, len(operation.Recipients))
	for index, recipient := range operation.Recipients {
		results[index] = RecoveryResult{Recipient: recipient, Status: Unresolved}
	}

	var observations []QueryObservation
	queryErrors := make([]error, len(operation.Recipients))
	for attempt := 1; attempt <= options.Attempts; attempt++ {
		for index, recipient := range operation.Recipients {
			if results[index].Status == Recovered || results[index].Status == Ambiguous {
				continue
			}
			candidates, observation, err := queryRecipient(ctx, client, operation, recipient)
			observation.Attempt = attempt
			observations = append(observations, observation)
			if err != nil {
				queryErrors[index] = err
				continue
			}
			queryErrors[index] = nil
			matches := matchingRecords(candidates, operation, recipient, correlationID, options.Tolerance)
			results[index].Candidates = matches
			switch len(matches) {
			case 0:
				results[index].Reason = "no matching Sweego log found within the recovery window"
			case 1:
				results[index].Status = Recovered
				results[index].SwgUID = matches[0].SwgUID
				results[index].Record = &matches[0]
			default:
				results[index].Status = Ambiguous
				results[index].Reason = "multiple log records satisfy the correlation criteria"
			}
		}
		if allRecipientsResolved(results) || attempt == options.Attempts {
			break
		}
		if options.RetryDelay > 0 {
			select {
			case <-time.After(options.RetryDelay):
			case <-ctx.Done():
				return results, observations, ctx.Err()
			}
		}
	}
	return results, observations, errors.Join(queryErrors...)
}

func queryRecipient(ctx context.Context, client *Client, operation RecoveryOperation, recipient string) ([]Record, QueryObservation, error) {
	result, err := client.Query(ctx, Request{
		Channel: "email", StartDate: operation.SubmittedAt.Add(-24 * time.Hour).Format("2006-01-02"),
		EndDate: operation.SubmittedAt.Add(24 * time.Hour).Format("2006-01-02"), SearchWord: recipient, Size: 500,
	})
	observation := QueryObservation{Recipient: recipient, Response: result, Err: err}
	if err != nil {
		return nil, observation, err
	}
	if result.Status < 200 || result.Status >= 300 {
		err := fmt.Errorf("logs query returned non-2xx status: %d", result.Status)
		observation.Err = err
		return nil, observation, err
	}
	var response Response
	if err := json.Unmarshal(result.Body, &response); err != nil {
		err = fmt.Errorf("decode logs response: %w", err)
		observation.Err = err
		return nil, observation, err
	}
	return response.Result, observation, nil
}

func allRecipientsResolved(results []RecoveryResult) bool {
	for _, result := range results {
		if result.Status == Unresolved {
			return false
		}
	}
	return true
}
