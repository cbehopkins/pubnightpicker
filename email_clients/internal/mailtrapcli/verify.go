package mailtrapcli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	"email_clients/clients"
	"email_clients/clients/mailtrap"
)

func (a *app) runVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	var to, correlationID, sentAtRaw string
	var tolerance time.Duration
	fs.StringVar(&to, "to", "", "recipient address")
	fs.StringVar(&correlationID, "message-id", "", "application correlation ID printed at submission")
	fs.StringVar(&sentAtRaw, "sent-at", "", "RFC3339 submission timestamp printed at submission")
	fs.DurationVar(&tolerance, "verify-tolerance", 5*time.Minute, "time window around the submission")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || strings.TrimSpace(correlationID) == "" || tolerance < 0 {
		return errors.New("verify requires --to, --message-id, --sent-at and a non-negative tolerance")
	}
	address, err := parseAddress(to)
	if err != nil {
		return fmt.Errorf("invalid --to: %w", err)
	}
	sentAt, err := time.Parse(time.RFC3339Nano, sentAtRaw)
	if err != nil {
		return fmt.Errorf("invalid --sent-at: %w", err)
	}
	client, cfg, err := a.client()
	if err != nil {
		return err
	}
	if cfg.Sandbox {
		return errors.New("verification uses production email logs and is not available in sandbox mode")
	}
	result, err := mailtrap.NewVerifier(client, tolerance).Verify(context.Background(), clients.VerifyRequest{
		CorrelationID: strings.TrimSpace(correlationID), Recipient: address.Email, SentAt: sentAt,
	})
	if err != nil {
		return err
	}
	if !result.Found {
		return errors.New("no matching provider log found; this is not proof of refusal")
	}
	fmt.Fprintf(a.out, "Found provider log\nPMUID: %s\n", result.PMUID)
	return nil
}
