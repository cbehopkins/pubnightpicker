package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/mail"
	"os"
	"strings"
	"time"

	"sweego_client/sweego"
)

type bulkOperation struct {
	TransactionID string
	SubmittedAt   time.Time
	Sender        sweego.EmailAddress
	Recipients    []bulkRecipient
}

type bulkRecipient struct {
	Email  string
	SwgUID string
}

type bulkResponse struct {
	TransactionID string
	SwgUIDs       map[string]string
}

type bulkRecoveryResult string

const (
	bulkRecovered  bulkRecoveryResult = "RECOVERED"
	bulkUnresolved bulkRecoveryResult = "UNRESOLVED"
	bulkAmbiguous  bulkRecoveryResult = "AMBIGUOUS"
)

type bulkRecipientResult struct {
	Recipient  string
	Status     bulkRecoveryResult
	SwgUID     string
	Record     *sweego.LogRecord
	Candidates []sweego.LogRecord
	Reason     string
}

func runBulkSend(args []string, client *sweego.Client, provider string) error {
	options, err := parseBulkOptions(args)
	if err != nil {
		return err
	}

	from, err := parseAddress(options.from)
	if err != nil {
		return fmt.Errorf("invalid --from: %w", err)
	}
	recipients, err := parseRecipients(options.recipients)
	if err != nil {
		return err
	}
	correlationID := options.messageID
	if correlationID == "" {
		correlationID, err = sweego.NewCorrelationID()
		if err != nil {
			return err
		}
	}

	request := sweego.BulkEmailRequest{
		Channel:      "email",
		From:         from,
		Provider:     provider,
		Subject:      options.subject,
		Recipients:   recipients,
		MessageTxt:   options.text,
		CampaignType: options.campaignType,
		TemplateID:   options.templateID,
		TemplateName: options.templateName,
		TemplateVars: options.templateVars,
		DryRun:       options.dryRun,
		Headers:      map[string]string{sweego.PubnightMessageIDHeader: correlationID},
	}

	operation := bulkOperation{Sender: from, SubmittedAt: time.Now()}
	for _, recipient := range recipients {
		operation.Recipients = append(operation.Recipients, bulkRecipient{Email: recipient.Email})
	}

	requestBody, _ := json.Marshal(request)
	fmt.Printf("Bulk operation\n  submitted_at: %s\n  operation_id: %s\n  sender: %s\n  recipients: %d\n\nBulk request:\n%s\n",
		operation.SubmittedAt.Format(time.RFC3339), correlationID, from.Email, len(recipients), requestBody)

	response, sendErr := client.SendBulkEmail(context.Background(), request)
	if sendErr != nil {
		fmt.Fprintln(osStderr, "bulk POST error:", sendErr)
	} else {
		fmt.Printf("Bulk POST response\n  HTTP status: %d\n  headers: %v\n  raw body: %s\n", response.Status, response.Headers, response.Body)
	}

	actual, parseErr := parseBulkResponse(response.Body)
	if parseErr != nil {
		fmt.Fprintln(osStderr, "bulk response parse warning:", parseErr)
	} else {
		operation.TransactionID = actual.TransactionID
		for index := range operation.Recipients {
			operation.Recipients[index].SwgUID = actual.SwgUIDs[operation.Recipients[index].Email]
		}
	}

	fmt.Printf("\nBulk operation\n  transaction_id: %s\n\nRecipients:\n", operation.TransactionID)
	for _, recipient := range operation.Recipients {
		fmt.Printf("  %s\n    swg_uid: %s\n", recipient.Email, valueOrUnknown(recipient.SwgUID))
	}

	recoveryOperation := operation
	if options.discardResponse {
		fmt.Println("\nLost-response simulation: response identifiers are hidden from recovery.")
		recoveryOperation.TransactionID = ""
		for index := range recoveryOperation.Recipients {
			recoveryOperation.Recipients[index].SwgUID = ""
		}
	}

	results, recoveryErr := recoverBulkLogs(context.Background(), client, recoveryOperation, correlationID, options)
	printBulkRecovery(results, actual, options.discardResponse)
	if sendErr != nil {
		return sendErr
	}
	if response.Status < 200 || response.Status >= 300 {
		return fmt.Errorf("non-2xx response: %d", response.Status)
	}
	return recoveryErr
}

// osStderr is replaceable in tests without changing the command's output contract.
var osStderr = os.Stderr

type bulkOptions struct {
	from, recipients, subject, text                                    string
	templateID, templateName, templateVarsRaw, campaignType, messageID string
	discardResponse                                                    bool
	dryRun                                                              bool
	tolerance, retryDelay                                              time.Duration
	attempts                                                           int
	templateVars                                                       map[string]any
}

func parseBulkOptions(args []string) (bulkOptions, error) {
	fs := flag.NewFlagSet("bulk-send", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var options bulkOptions
	fs.StringVar(&options.from, "from", "", "from email address (optionally with display name)")
	fs.StringVar(&options.recipients, "to", "", "comma-separated recipient email addresses")
	fs.StringVar(&options.subject, "subject", "", "email subject")
	fs.StringVar(&options.text, "text", "", "plain text email body")
	fs.StringVar(&options.templateID, "template-id", "", "provider template identifier, if used")
	fs.StringVar(&options.templateName, "template-name", "", "provider template name, if used")
	fs.StringVar(&options.templateVarsRaw, "template-vars", "", "template variables as a JSON object")
	fs.StringVar(&options.campaignType, "campaign-type", "transac", "Sweego campaign type")
	fs.StringVar(&options.messageID, "message-id", "", "override the generated operation correlation value")
	fs.BoolVar(&options.discardResponse, "discard-response", false, "hide response identifiers from the recovery path")
	fs.BoolVar(&options.dryRun, "dry-run", false, "ask Sweego to accept the request without actually sending the email")
	fs.DurationVar(&options.tolerance, "recovery-window", defaultVerifyTolerance, "timestamp tolerance around submission")
	fs.DurationVar(&options.retryDelay, "retry-delay", 30*time.Second, "delay between log queries")
	fs.IntVar(&options.attempts, "attempts", 10, "number of log-query attempts")
	if err := fs.Parse(args); err != nil {
		return bulkOptions{}, err
	}
	if strings.TrimSpace(options.from) == "" || strings.TrimSpace(options.recipients) == "" {
		return bulkOptions{}, errors.New("--from and --to are required")
	}
	if options.attempts < 1 || options.retryDelay < 0 || options.tolerance < 0 {
		return bulkOptions{}, errors.New("--attempts must be positive and timing values cannot be negative")
	}
	if options.templateVarsRaw != "" {
		if err := json.Unmarshal([]byte(options.templateVarsRaw), &options.templateVars); err != nil {
			return bulkOptions{}, fmt.Errorf("invalid --template-vars JSON: %w", err)
		}
	}
	return options, nil
}

func parseRecipients(raw string) ([]sweego.EmailAddress, error) {
	var recipients []sweego.EmailAddress
	for _, value := range strings.Split(raw, ",") {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		address, err := parseAddress(value)
		if err != nil {
			return nil, fmt.Errorf("invalid recipient %q: %w", value, err)
		}
		recipients = append(recipients, address)
	}
	if len(recipients) < 2 {
		return nil, errors.New("--to must contain at least two recipients")
	}
	return recipients, nil
}

func parseBulkResponse(body []byte) (bulkResponse, error) {
	var root any
	if len(body) == 0 {
		return bulkResponse{}, errors.New("empty response body")
	}
	if err := json.Unmarshal(body, &root); err != nil {
		return bulkResponse{}, fmt.Errorf("decode response JSON: %w", err)
	}
	response := bulkResponse{SwgUIDs: map[string]string{}}
	collectBulkIdentifiers(root, &response)
	if response.TransactionID == "" && len(response.SwgUIDs) == 0 {
		return response, errors.New("no transaction_id or per-recipient swg_uid found; raw response retained above")
	}
	return response, nil
}

func collectBulkIdentifiers(value any, response *bulkResponse) {
	switch current := value.(type) {
	case map[string]any:
		var recipient, swgUID string
		for key, child := range current {
			switch strings.ToLower(strings.ReplaceAll(key, "-", "_")) {
			case "transaction_id", "transactionid":
				if text, ok := child.(string); ok && response.TransactionID == "" {
					response.TransactionID = text
				}
			case "swg_uids", "swguids":
				if ids, ok := child.(map[string]any); ok {
					for recipient, identifier := range ids {
						if text, ok := identifier.(string); ok {
							response.SwgUIDs[recipient] = text
						}
					}
				}
			case "recipient", "email", "to":
				if text, ok := child.(string); ok {
					recipient = text
				}
			case "swg_uid", "swguid":
				if text, ok := child.(string); ok {
					swgUID = text
				}
			}
			collectBulkIdentifiers(child, response)
		}
		if recipient != "" && swgUID != "" {
			response.SwgUIDs[recipient] = swgUID
		}
	case []any:
		for _, child := range current {
			collectBulkIdentifiers(child, response)
		}
	}
}

func recoverBulkLogs(ctx context.Context, client *sweego.Client, operation bulkOperation, correlationID string, options bulkOptions) ([]bulkRecipientResult, error) {
	results := make([]bulkRecipientResult, len(operation.Recipients))
	for index, recipient := range operation.Recipients {
		results[index] = bulkRecipientResult{Recipient: recipient.Email, Status: bulkUnresolved}
	}
	var queryErr error
	for attempt := 1; attempt <= options.attempts; attempt++ {
		fmt.Printf("\nLog recovery attempt %d/%d\n", attempt, options.attempts)
		for index, recipient := range operation.Recipients {
			if results[index].Status == bulkRecovered || results[index].Status == bulkAmbiguous {
				continue
			}
			candidates, err := queryBulkRecipientLogs(ctx, client, operation, recipient.Email)
			if err != nil {
				queryErr = err
				continue
			}
			matches := matchingBulkLogs(candidates, operation, recipient.Email, correlationID, options)
			results[index].Candidates = matches
			switch len(matches) {
			case 0:
				results[index].Reason = "no matching Sweego log found within the recovery window"
			case 1:
				results[index].Status = bulkRecovered
				results[index].SwgUID = matches[0].SwgUID
				results[index].Record = &matches[0]
			default:
				results[index].Status = bulkAmbiguous
				results[index].Reason = "multiple log records satisfy the correlation criteria"
			}
		}
		if allBulkRecipientsResolved(results) || attempt == options.attempts {
			break
		}
		if options.retryDelay > 0 {
			time.Sleep(options.retryDelay)
		}
	}
	return results, queryErr
}

func queryBulkRecipientLogs(ctx context.Context, client *sweego.Client, operation bulkOperation, recipient string) ([]sweego.LogRecord, error) {
	status, body, err := client.QueryLogs(ctx, sweego.LogsRequest{
		Channel: "email", StartDate: operation.SubmittedAt.Add(-24 * time.Hour).Format("2006-01-02"),
		EndDate: operation.SubmittedAt.Add(24 * time.Hour).Format("2006-01-02"), SearchWord: recipient, Size: 500,
	})
	if err != nil {
		return nil, err
	}
	fmt.Printf("Raw relevant log response for %s: %s\n", recipient, body)
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("logs query returned non-2xx status: %d", status)
	}
	var response sweego.LogsResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode logs response: %w", err)
	}
	return response.Result, nil
}

func matchingBulkLogs(records []sweego.LogRecord, operation bulkOperation, recipient, correlationID string, options bulkOptions) []sweego.LogRecord {
	matches := make([]sweego.LogRecord, 0)
	for _, record := range records {
		if !sameEmail(record.EmailTo, recipient) || !sameEmail(record.EmailFrom, operation.Sender.Email) || record.Channel != "" && record.Channel != "email" {
			continue
		}
		created, err := parseSweegoTime(record.EmailCreation)
		if err != nil || created.Before(operation.SubmittedAt.Add(-options.tolerance)) || created.After(operation.SubmittedAt.Add(options.tolerance)) {
			continue
		}
		if operation.TransactionID != "" && record.TransactionID != "" && operation.TransactionID != record.TransactionID {
			continue
		}
		if value, ok := bulkHeaderValue(record.Headers, sweego.PubnightMessageIDHeader); ok && value != correlationID {
			continue
		}
		matches = append(matches, record)
	}
	return matches
}

func bulkHeaderValue(headers map[string]any, name string) (string, bool) {
	for key, raw := range headers {
		if !strings.EqualFold(key, name) {
			continue
		}
		value, ok := raw.(string)
		return value, ok
	}
	return "", false
}

func printBulkRecovery(results []bulkRecipientResult, actual bulkResponse, hidden bool) {
	fmt.Println("\nBulk recovery report")
	for _, result := range results {
		fmt.Printf("  %s: %s", result.Recipient, result.Status)
		if result.SwgUID != "" {
			fmt.Printf(" swg_uid=%s", result.SwgUID)
		}
		if result.Reason != "" {
			fmt.Printf(" (%s)", result.Reason)
		}
		fmt.Println()
	}
	if hidden {
		complete := len(results) > 0
		for _, result := range results {
			if result.Status != bulkRecovered || actual.SwgUIDs[result.Recipient] != result.SwgUID {
				complete = false
			}
		}
		if complete {
			fmt.Println("Result: COMPLETE RECOVERY")
		}
	}
}

func allBulkRecipientsResolved(results []bulkRecipientResult) bool {
	for _, result := range results {
		if result.Status == bulkUnresolved {
			return false
		}
	}
	return true
}

func parseSweegoTime(raw string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported Sweego timestamp %q", raw)
}

func sameEmail(left, right string) bool {
	leftAddress, leftErr := mail.ParseAddress(left)
	rightAddress, rightErr := mail.ParseAddress(right)
	if leftErr != nil || rightErr != nil {
		return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
	}
	return strings.EqualFold(leftAddress.Address, rightAddress.Address)
}

func valueOrUnknown(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}
