package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sweego_client/sweego"
)

// bulkSendDocument is the on-disk target format. Field names follow the
// existing bulk_request.json rather than Sweego's wire names. Exactly one of
// Template and Body supplies the message content.
type bulkSendDocument struct {
	Template     string           `json:"template"`
	Body         string           `json:"body"`
	Subject      string           `json:"subject"`
	From         string           `json:"from"`
	CampaignType string           `json:"campaign-type"`
	Targets      []bulkSendTarget `json:"targets"`
}

type bulkSendTarget struct {
	Dest string         `json:"dest"`
	Vars map[string]any `json:"vars"`
}

type bulkSendOptions struct {
	recoveryOptions
	from      string
	messageID string
	dryRun    bool
}

func runBulkSendDocument(args []string, client *sweego.Client, provider string) error {
	fs := flag.NewFlagSet("bulk-send-json", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var options bulkSendOptions
	fs.StringVar(&options.from, "from", "", "from email address, overriding the document's from value")
	fs.StringVar(&options.messageID, "message-id", "", "override the generated operation correlation value")
	fs.BoolVar(&options.dryRun, "dry-run", false, "ask Sweego to accept the request without actually sending the email")
	registerRecoveryFlags(fs, &options.recoveryOptions)

	positionals, err := parseFlagsWithPositionals(fs, args)
	if err != nil {
		return err
	}
	if len(positionals) != 1 {
		return errors.New("exactly one <targets-file> argument is required")
	}
	if err := options.validate(); err != nil {
		return err
	}

	document, messageTxt, err := loadBulkSendDocument(positionals[0])
	if err != nil {
		return err
	}

	fromRaw := options.from
	if strings.TrimSpace(fromRaw) == "" {
		fromRaw = document.From
	}
	if strings.TrimSpace(fromRaw) == "" {
		return errors.New("a sender is required: set \"from\" in the document or pass --from")
	}
	from, err := parseAddress(fromRaw)
	if err != nil {
		return fmt.Errorf("invalid sender %q: %w", fromRaw, err)
	}

	recipients, err := bulkSendRecipients(document.Targets)
	if err != nil {
		return err
	}
	if len(recipients) == 0 {
		fmt.Println("No targets in document: nothing to send.")
		return nil
	}

	correlationID := strings.TrimSpace(options.messageID)
	if correlationID == "" {
		correlationID, err = sweego.NewCorrelationID()
		if err != nil {
			return err
		}
	}

	campaignType := document.CampaignType
	if strings.TrimSpace(campaignType) == "" {
		campaignType = "transac"
	}

	// Exactly one of these is populated; the loader enforces that.
	request := sweego.BulkEmailRequest{
		Channel:      "email",
		From:         from,
		Provider:     provider,
		Subject:      document.Subject,
		Recipients:   recipients,
		MessageTxt:   messageTxt,
		CampaignType: campaignType,
		TemplateID:   document.Template,
		DryRun:       options.dryRun,
		Headers:      map[string]string{sweego.PubnightMessageIDHeader: correlationID},
	}

	operation := bulkOperation{Sender: from, SubmittedAt: time.Now()}
	for _, recipient := range recipients {
		operation.Recipients = append(operation.Recipients, bulkRecipient{Email: recipient.Email})
	}

	source := "template " + document.Template
	if document.Body != "" {
		source = "body file " + document.Body
	}
	requestBody, _ := json.Marshal(request)
	fmt.Printf("Bulk document operation\n  submitted_at: %s\n  operation_id: %s\n  sender: %s\n  content: %s\n  recipients: %d\n\nBulk request:\n%s\n",
		operation.SubmittedAt.Format(time.RFC3339), correlationID, from.Email, source, len(recipients), requestBody)

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

	fmt.Printf("\nBulk document operation\n  transaction_id: %s\n\nRecipients:\n", operation.TransactionID)
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

	results, recoveryErr := recoverBulkLogs(context.Background(), client, recoveryOperation, correlationID, options.recoveryOptions)
	printBulkRecovery(results, actual, options.discardResponse)

	if sendErr != nil {
		return sendErr
	}
	if response.Status < 200 || response.Status >= 300 {
		return fmt.Errorf("non-2xx response: %d", response.Status)
	}
	return recoveryErr
}

// loadBulkSendDocument returns the document and, when "body" is used, the text
// file's contents. The body path is resolved relative to the document.
func loadBulkSendDocument(path string) (bulkSendDocument, string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return bulkSendDocument{}, "", fmt.Errorf("read targets file: %w", err)
	}

	var document bulkSendDocument
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return bulkSendDocument{}, "", fmt.Errorf("decode targets JSON: %w", err)
	}

	hasTemplate := strings.TrimSpace(document.Template) != ""
	hasBody := strings.TrimSpace(document.Body) != ""
	switch {
	case hasTemplate && hasBody:
		return bulkSendDocument{}, "", errors.New(`"template" and "body" are mutually exclusive: supply exactly one`)
	case !hasTemplate && !hasBody:
		return bulkSendDocument{}, "", errors.New(`one of "template" (a Sweego template UUID) or "body" (a plain-text file path) is required`)
	}

	// Sweego rejects a bulk send without a subject, template or not.
	if strings.TrimSpace(document.Subject) == "" {
		return bulkSendDocument{}, "", errors.New(`"subject" is required`)
	}

	if !hasBody {
		return document, "", nil
	}

	bodyPath := document.Body
	if !filepath.IsAbs(bodyPath) {
		bodyPath = filepath.Join(filepath.Dir(path), bodyPath)
	}
	messageTxt, err := os.ReadFile(bodyPath)
	if err != nil {
		return bulkSendDocument{}, "", fmt.Errorf("read body file: %w", err)
	}
	return document, string(messageTxt), nil
}

func bulkSendRecipients(targets []bulkSendTarget) ([]sweego.BulkRecipient, error) {
	recipients := make([]sweego.BulkRecipient, 0, len(targets))
	for index, target := range targets {
		address, err := parseAddress(target.Dest)
		if err != nil {
			return nil, fmt.Errorf("invalid target %d dest %q: %w", index, target.Dest, err)
		}
		recipients = append(recipients, sweego.BulkRecipient{
			Email:     address.Email,
			Name:      address.Name,
			Variables: target.Vars,
		})
	}
	return recipients, nil
}
