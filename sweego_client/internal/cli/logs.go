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

func runLogs(args []string, client *sweego.Client) error {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var uid, to, date string
	fs.StringVar(&uid, "uid", "", "Sweego swg_uid")
	fs.StringVar(&to, "to", "", "recipient email")
	fs.StringVar(&date, "date", "", "date in YYYY-MM-DD")
	if err := fs.Parse(args); err != nil {
		return err
	}
	uid, to, date = strings.TrimSpace(uid), strings.TrimSpace(to), strings.TrimSpace(date)
	if uid == "" && to == "" {
		return errors.New("at least one of --uid or --to is required")
	}
	if date != "" {
		if _, err := time.Parse("2006-01-02", date); err != nil {
			return fmt.Errorf("invalid --date, expected YYYY-MM-DD: %w", err)
		}
	}
	req := logs.Request{Channel: "email", StartDate: date, EndDate: date}
	if uid != "" {
		req.SearchWord = uid
	} else {
		req.SearchWord = to
	}
	response, err := logs.NewClient(client).Query(context.Background(), req)
	if err != nil {
		return err
	}
	printHTTPResult(response.Status, response.Body)
	if response.Status < 200 || response.Status >= 300 {
		return fmt.Errorf("non-2xx response: %d", response.Status)
	}
	return nil
}

func runVerify(args []string, client *sweego.Client) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var to, messageID, sentAtRaw string
	var tolerance time.Duration
	fs.StringVar(&to, "to", "", "recipient email address")
	fs.StringVar(&messageID, "message-id", "", "PubNight correlation ID to search for")
	fs.StringVar(&sentAtRaw, "sent-at", "", "RFC3339 timestamp of the original send attempt (defaults to now)")
	fs.DurationVar(&tolerance, "tolerance", defaultVerifyTolerance, "time window (+/-) around --sent-at to search Sweego logs")
	if err := fs.Parse(args); err != nil {
		return err
	}
	to, messageID = strings.TrimSpace(to), strings.TrimSpace(messageID)
	if to == "" || messageID == "" {
		return errors.New("--to and --message-id are required")
	}
	sentAt := time.Now()
	if sentAtRaw != "" {
		parsed, err := time.Parse(time.RFC3339, sentAtRaw)
		if err != nil {
			return fmt.Errorf("invalid --sent-at, expected RFC3339: %w", err)
		}
		sentAt = parsed
	}
	result := logs.NewVerifier(logs.NewClient(client), tolerance).VerifyMessage(context.Background(), messageID, to, sentAt)
	printVerificationResult(result)
	if result.Status == logs.VerificationQueryError {
		return result.Err
	}
	return nil
}
