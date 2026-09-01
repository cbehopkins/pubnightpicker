package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"sweego_client/sweego"
	"sweego_client/sweego/logs"
)

func runSend(args []string, client *sweego.Client, provider string) error {
	fs := flag.NewFlagSet("send", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var from, to, subject, text, messageID string
	var tolerance time.Duration
	var dryRun bool
	fs.StringVar(&from, "from", "", "from email address (optionally with display name)")
	fs.StringVar(&to, "to", "", "recipient email address")
	fs.StringVar(&subject, "subject", "", "email subject")
	fs.StringVar(&text, "text", "", "plain text email body")
	fs.StringVar(&messageID, "message-id", "", "override the generated X-Pubnight-Message-ID correlation value")
	fs.DurationVar(&tolerance, "verify-tolerance", defaultVerifyTolerance, "time window (+/-) around the send attempt to search Sweego logs")
	fs.BoolVar(&dryRun, "dry-run", false, "ask Sweego to accept the request without actually sending the email")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(from) == "" || strings.TrimSpace(to) == "" || strings.TrimSpace(subject) == "" || strings.TrimSpace(text) == "" {
		return errors.New("--from, --to, --subject, and --text are required")
	}
	fromAddr, err := parseAddress(from)
	if err != nil {
		return fmt.Errorf("invalid --from: %w", err)
	}
	toAddr, err := parseAddress(to)
	if err != nil {
		return fmt.Errorf("invalid --to: %w", err)
	}
	correlationID := strings.TrimSpace(messageID)
	if correlationID == "" {
		correlationID, err = sweego.NewCorrelationID()
		if err != nil {
			return err
		}
	}
	req := sweego.SendEmailRequest{Channel: "email", From: fromAddr, Provider: provider, Subject: subject, Recipients: []sweego.EmailAddress{toAddr}, MessageTxt: text, CampaignType: "transac", DryRun: dryRun, Headers: map[string]string{sweego.PubnightMessageIDHeader: correlationID}}
	fmt.Printf("PubNight message ID: %s\n", correlationID)
	ctx := context.Background()
	sentAt := time.Now()
	response, sendErr := client.SendEmail(ctx, req)
	if sendErr != nil {
		fmt.Fprintln(os.Stderr, "send error:", sendErr)
	} else {
		printHTTPResult(response.Status, response.Body)
	}
	result := logs.NewVerifier(logs.NewClient(client), tolerance).VerifyMessage(ctx, correlationID, toAddr.Email, sentAt)
	printVerificationResult(result)
	if sendErr != nil {
		return sendErr
	}
	if response.Status < 200 || response.Status >= 300 {
		return fmt.Errorf("non-2xx response: %d", response.Status)
	}
	return nil
}
