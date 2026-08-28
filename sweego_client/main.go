package main

import (
	"bytes"
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

const defaultBaseURL = "https://api.sweego.io"
const defaultVerifyTolerance = 5 * time.Minute

func main() {
	if len(os.Args) < 2 {
		printUsage(os.Stderr)
		os.Exit(2)
	}

	cfg, err := loadConfigFromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(2)
	}

	client := sweego.NewClient(cfg.BaseURL, cfg.Token, 15*time.Second)

	switch os.Args[1] {
	case "send":
		if err := runSend(os.Args[2:], client, cfg.Provider); err != nil {
			fmt.Fprintln(os.Stderr, "send error:", err)
			os.Exit(1)
		}
	case "logs":
		if err := runLogs(os.Args[2:], client); err != nil {
			fmt.Fprintln(os.Stderr, "logs error:", err)
			os.Exit(1)
		}
	case "verify":
		if err := runVerify(os.Args[2:], client); err != nil {
			fmt.Fprintln(os.Stderr, "verify error:", err)
			os.Exit(1)
		}
	case "bulk-send":
		if err := runBulkSend(os.Args[2:], client, cfg.Provider); err != nil {
			fmt.Fprintln(os.Stderr, "bulk-send error:", err)
			os.Exit(1)
		}
	case "bulk-send-json", "bulk-send-template":
		if err := runBulkSendDocument(os.Args[2:], client, cfg.Provider); err != nil {
			fmt.Fprintln(os.Stderr, "bulk-send-json error:", err)
			os.Exit(1)
		}
	case "template-upload":
		clientUUID, err := requireClientUUID(cfg)
		if err == nil {
			err = runTemplateUpload(os.Args[2:], client, clientUUID)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "template-upload error:", err)
			os.Exit(1)
		}
	case "template-update":
		clientUUID, err := requireClientUUID(cfg)
		if err == nil {
			err = runTemplateUpdate(os.Args[2:], client, clientUUID)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "template-update error:", err)
			os.Exit(1)
		}
	case "template-delete":
		clientUUID, err := requireClientUUID(cfg)
		if err == nil {
			err = runTemplateDelete(os.Args[2:], client, clientUUID)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "template-delete error:", err)
			os.Exit(1)
		}
	default:
		printUsage(os.Stderr)
		os.Exit(2)
	}
}

type config struct {
	Token      string
	Provider   string
	BaseURL    string
	ClientUUID string
}

// requireClientUUID is checked per command because only the template endpoints
// are scoped by client.
func requireClientUUID(cfg config) (string, error) {
	if cfg.ClientUUID == "" {
		return "", errors.New("SWEEGO_CLIENT_UUID is required for template commands")
	}
	return cfg.ClientUUID, nil
}

func loadConfigFromEnv() (config, error) {
	token := strings.TrimSpace(os.Getenv("SWEEGO_TOKEN"))
	if token == "" {
		return config{}, errors.New("SWEEGO_TOKEN is required")
	}

	provider := strings.TrimSpace(os.Getenv("SWEEGO_PROVIDER"))
	if provider == "" {
		return config{}, errors.New("SWEEGO_PROVIDER is required")
	}

	baseURL := strings.TrimSpace(os.Getenv("SWEEGO_BASE_URL"))
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	return config{
		Token:      token,
		Provider:   provider,
		BaseURL:    strings.TrimRight(baseURL, "/"),
		ClientUUID: strings.TrimSpace(os.Getenv("SWEEGO_CLIENT_UUID")),
	}, nil
}

func runSend(args []string, client *sweego.Client, provider string) error {
	fs := flag.NewFlagSet("send", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var from string
	var to string
	var subject string
	var text string
	var messageID string
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

	req := sweego.SendEmailRequest{
		Channel:      "email",
		From:         fromAddr,
		Provider:     provider,
		Subject:      subject,
		Recipients:   []sweego.EmailAddress{toAddr},
		MessageTxt:   text,
		CampaignType: "transac",
		DryRun:       dryRun,
		Headers:      map[string]string{sweego.PubnightMessageIDHeader: correlationID},
	}

	fmt.Printf("PubNight message ID: %s\n", correlationID)

	ctx := context.Background()
	sentAt := time.Now()
	response, sendErr := client.SendEmail(ctx, req)
	if sendErr != nil {
		fmt.Fprintln(os.Stderr, "send error:", sendErr)
	} else {
		printHTTPResult(response.Status, response.Body)
	}

	// Verify independently of the outcome above: the response is never a
	// precondition for the log check, only its (optional) trigger.
	verifier := sweego.NewLogVerifier(client, tolerance)
	result := verifier.VerifyMessage(ctx, correlationID, toAddr.Email, sentAt)
	printVerificationResult(result)

	if sendErr != nil {
		return sendErr
	}
	if response.Status < 200 || response.Status >= 300 {
		return fmt.Errorf("non-2xx response: %d", response.Status)
	}

	return nil
}

func runLogs(args []string, client *sweego.Client) error {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var uid string
	var to string
	var date string

	fs.StringVar(&uid, "uid", "", "Sweego swg_uid")
	fs.StringVar(&to, "to", "", "recipient email")
	fs.StringVar(&date, "date", "", "date in YYYY-MM-DD")

	if err := fs.Parse(args); err != nil {
		return err
	}

	uid = strings.TrimSpace(uid)
	to = strings.TrimSpace(to)
	date = strings.TrimSpace(date)

	if uid == "" && to == "" {
		return errors.New("at least one of --uid or --to is required")
	}

	if date != "" {
		if _, err := time.Parse("2006-01-02", date); err != nil {
			return fmt.Errorf("invalid --date, expected YYYY-MM-DD: %w", err)
		}
	}

	req := sweego.LogsRequest{Channel: "email", StartDate: date, EndDate: date}
	if uid != "" {
		req.SearchWord = uid
	} else {
		req.SearchWord = to
	}

	ctx := context.Background()
	status, body, err := client.QueryLogs(ctx, req)
	if err != nil {
		return err
	}

	printHTTPResult(status, body)
	if status < 200 || status >= 300 {
		return fmt.Errorf("non-2xx response: %d", status)
	}

	return nil
}

func runVerify(args []string, client *sweego.Client) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var to string
	var messageID string
	var sentAtRaw string
	var tolerance time.Duration

	fs.StringVar(&to, "to", "", "recipient email address")
	fs.StringVar(&messageID, "message-id", "", "PubNight correlation ID to search for")
	fs.StringVar(&sentAtRaw, "sent-at", "", "RFC3339 timestamp of the original send attempt (defaults to now)")
	fs.DurationVar(&tolerance, "tolerance", defaultVerifyTolerance, "time window (+/-) around --sent-at to search Sweego logs")

	if err := fs.Parse(args); err != nil {
		return err
	}

	to = strings.TrimSpace(to)
	messageID = strings.TrimSpace(messageID)
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

	verifier := sweego.NewLogVerifier(client, tolerance)
	result := verifier.VerifyMessage(context.Background(), messageID, to, sentAt)
	printVerificationResult(result)

	if result.Status == sweego.VerificationQueryError {
		return result.Err
	}
	return nil
}

func parseAddress(raw string) (sweego.EmailAddress, error) {
	addr, err := mail.ParseAddress(raw)
	if err != nil {
		return sweego.EmailAddress{}, err
	}
	return sweego.EmailAddress{
		Email: addr.Address,
		Name:  addr.Name,
	}, nil
}

// parseFlagsWithPositionals lets flags appear after positional arguments, which
// flag.FlagSet does not support on its own.
func parseFlagsWithPositionals(fs *flag.FlagSet, args []string) ([]string, error) {
	var positionals []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positionals, nil
		}
		positionals = append(positionals, rest[0])
		args = rest[1:]
	}
}

func printHTTPResult(status int, body []byte) {
	fmt.Printf("HTTP status: %d\n", status)
	fmt.Println(prettyJSON(body))
}

func printVerificationResult(result sweego.VerificationResult) {
	fmt.Printf("Verification: %s\n", result.Status)
	switch result.Status {
	case sweego.VerificationFound:
		fmt.Printf("  correlation_id: %s\n", result.CorrelationID)
		fmt.Printf("  recipient:      %s\n", result.Recipient)
		fmt.Printf("  transaction_id: %s\n", result.TransactionID)
		fmt.Printf("  swg_uid:        %s\n", result.SwgUID)
		fmt.Printf("  email_status:   %s\n", result.EmailStatus)
	case sweego.VerificationNotFound:
		fmt.Printf("  correlation_id: %s\n", result.CorrelationID)
		fmt.Printf("  recipient:      %s\n", result.Recipient)
	case sweego.VerificationQueryError:
		fmt.Printf("  error: %v\n", result.Err)
	}
}

func prettyJSON(raw []byte) string {
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err == nil {
		return out.String()
	}
	return string(raw)
}

func printUsage(out *os.File) {
	fmt.Fprintln(out, "Usage:")
	fmt.Fprintln(out, "  sweego_client send --from <email> --to <email> --subject <text> --text <text> [--message-id <id>] [--verify-tolerance <duration>] [--dry-run]")
	fmt.Fprintln(out, "  sweego_client logs [--uid <value>] [--to <email>] [--date <YYYY-MM-DD>]")
	fmt.Fprintln(out, "  sweego_client verify --to <email> --message-id <id> [--sent-at <RFC3339>] [--tolerance <duration>]")
	fmt.Fprintln(out, "  sweego_client bulk-send --from <email> --to <email,email,...> [--subject <text>] [--text <text>] [--discard-response] [--dry-run]")
	fmt.Fprintln(out, "  sweego_client bulk-send-json <targets.json> [--from <email>] [--message-id <id>] [--discard-response] [--dry-run]")
	fmt.Fprintln(out, "  sweego_client template-upload <template-file> [--name <text>]")
	fmt.Fprintln(out, "  sweego_client template-update <template-uuid> <template-file> [--name <text>]")
	fmt.Fprintln(out, "  sweego_client template-delete <template-uuid>")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Environment:")
	fmt.Fprintln(out, "  SWEEGO_TOKEN (required)")
	fmt.Fprintln(out, "  SWEEGO_PROVIDER (required)")
	fmt.Fprintln(out, "  SWEEGO_CLIENT_UUID (required by the template commands)")
	fmt.Fprintln(out, "  SWEEGO_BASE_URL (optional, defaults to https://api.sweego.io)")
}
