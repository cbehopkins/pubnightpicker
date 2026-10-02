package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"email_clients/clients"
	"email_clients/clients/sweego"
	"email_clients/clients/sweego/logs"
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
	email := clients.Email{
		From: fromAddr, To: []clients.Recipient{{Address: toAddr}}, Subject: subject, Text: text,
		Headers: map[string]string{sweego.PubnightMessageIDHeader: correlationID},
	}
	fmt.Printf("PubNight message ID: %s\n", correlationID)
	ctx := context.Background()
	sentAt := time.Now()
	result, sendErr := client.WithSendOptions(sweego.SendOptions{
		Provider: provider, CampaignType: "transac", DryRun: dryRun,
	}).Send(ctx, email)
	if sendErr != nil {
		fmt.Fprintln(os.Stderr, "send error:", sendErr)
	} else {
		fmt.Printf("PMUID: %s\n", result.Recipients[0].PMUID)
	}
	verification, verifyErr := logs.NewVerifier(logs.NewClient(client), tolerance).Verify(ctx, clients.VerifyRequest{
		CorrelationID: correlationID, Recipient: toAddr.Email, SentAt: sentAt,
	})
	if verifyErr != nil {
		fmt.Fprintln(os.Stderr, "verification error:", verifyErr)
	} else {
		printVerificationResult(verification)
	}
	if sendErr != nil {
		return sendErr
	}
	return verifyErr
}
