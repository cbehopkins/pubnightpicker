package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"sweego_client/sweego"
	"sweego_client/sweego/logs"
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

	results, observations, recoveryErr := recoverBulkLogs(context.Background(), client, recoveryOperation, correlationID, options.recoveryOptions)
	printLogObservations(observations, options.attempts)
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

// recoveryOptions is the subset of settings the log-recovery path needs, shared
// by the flag-driven and template-driven bulk commands.
type recoveryOptions struct {
	discardResponse bool
	tolerance       time.Duration
	retryDelay      time.Duration
	attempts        int
}

type bulkOptions struct {
	recoveryOptions
	from, recipients, subject, text     string
	templateID, campaignType, messageID string
	dryRun                              bool
}

func registerRecoveryFlags(fs *flag.FlagSet, options *recoveryOptions) {
	fs.BoolVar(&options.discardResponse, "discard-response", false, "hide response identifiers from the recovery path")
	fs.DurationVar(&options.tolerance, "recovery-window", defaultVerifyTolerance, "timestamp tolerance around submission")
	fs.DurationVar(&options.retryDelay, "retry-delay", 30*time.Second, "delay between log queries")
	fs.IntVar(&options.attempts, "attempts", 10, "number of log-query attempts")
}

func (o recoveryOptions) validate() error {
	if o.attempts < 1 || o.retryDelay < 0 || o.tolerance < 0 {
		return errors.New("--attempts must be positive and timing values cannot be negative")
	}
	return nil
}

func parseBulkOptions(args []string) (bulkOptions, error) {
	fs := flag.NewFlagSet("bulk-send", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var options bulkOptions
	fs.StringVar(&options.from, "from", "", "from email address (optionally with display name)")
	fs.StringVar(&options.recipients, "to", "", "comma-separated recipient email addresses")
	fs.StringVar(&options.subject, "subject", "", "email subject")
	fs.StringVar(&options.text, "text", "", "plain text email body")
	fs.StringVar(&options.templateID, "template-id", "", "Sweego template UUID, if used")
	fs.StringVar(&options.campaignType, "campaign-type", "transac", "Sweego campaign type")
	fs.StringVar(&options.messageID, "message-id", "", "override the generated operation correlation value")
	fs.BoolVar(&options.dryRun, "dry-run", false, "ask Sweego to accept the request without actually sending the email")
	registerRecoveryFlags(fs, &options.recoveryOptions)
	if err := fs.Parse(args); err != nil {
		return bulkOptions{}, err
	}
	if strings.TrimSpace(options.from) == "" || strings.TrimSpace(options.recipients) == "" {
		return bulkOptions{}, errors.New("--from and --to are required")
	}
	if err := options.validate(); err != nil {
		return bulkOptions{}, err
	}
	return options, nil
}

func parseRecipients(raw string) ([]sweego.BulkRecipient, error) {
	var recipients []sweego.BulkRecipient
	for value := range strings.SplitSeq(raw, ",") {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		address, err := parseAddress(value)
		if err != nil {
			return nil, fmt.Errorf("invalid recipient %q: %w", value, err)
		}
		recipients = append(recipients, sweego.BulkRecipient{Email: address.Email, Name: address.Name})
	}
	if len(recipients) == 0 {
		return nil, errors.New("--to must contain at least one recipient")
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

func recoverBulkLogs(ctx context.Context, client *sweego.Client, operation bulkOperation, correlationID string, options recoveryOptions) ([]logs.RecoveryResult, []logs.QueryObservation, error) {
	recipients := make([]string, len(operation.Recipients))
	for index, recipient := range operation.Recipients {
		recipients[index] = recipient.Email
	}
	return logs.Recover(ctx, logs.NewClient(client), logs.RecoveryOperation{
		TransactionID: operation.TransactionID,
		SubmittedAt:   operation.SubmittedAt,
		Sender:        operation.Sender.Email,
		Recipients:    recipients,
	}, correlationID, logs.RecoveryOptions{
		Tolerance: options.tolerance, RetryDelay: options.retryDelay, Attempts: options.attempts,
	})
}

func printLogObservations(observations []logs.QueryObservation, attempts int) {
	lastAttempt := 0
	for _, observation := range observations {
		if observation.Attempt != lastAttempt {
			fmt.Printf("\nLog recovery attempt %d/%d\n", observation.Attempt, attempts)
			lastAttempt = observation.Attempt
		}
		fmt.Printf("Raw relevant log response for %s: %s\n", observation.Recipient, observation.Response.Body)
	}
}

func printBulkRecovery(results []logs.RecoveryResult, actual bulkResponse, hidden bool) {
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
			if result.Status != logs.Recovered || actual.SwgUIDs[result.Recipient] != result.SwgUID {
				complete = false
			}
		}
		if complete {
			fmt.Println("Result: COMPLETE RECOVERY")
		}
	}
}

func valueOrUnknown(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}
