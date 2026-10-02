package email

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"cellar/pkg/cellar"
	durableemail "durable_email"
	"email_clients/clients"
	"email_clients/clients/dummy"
	"email_clients/clients/mailtrap"
	"email_clients/clients/sweego"
	sweegologs "email_clients/clients/sweego/logs"
	"last_orders/internal/lastorders/components/ratelimit"

	sdk "github.com/mailtrap/mailtrap-go"
)

// ClientKind selects the email provider the plugin sends through.
type ClientKind string

const (
	// ClientDummy logs each email instead of sending it.
	ClientDummy ClientKind = "dummy"
	// ClientMailtrap sends and verifies email through Mailtrap.
	ClientMailtrap ClientKind = "mailtrap"
	// ClientSweego sends email through Sweego and verifies delivery through its logs API.
	ClientSweego ClientKind = "sweego"
)

type Options struct {
	Client                  ClientKind
	Tokens                  ratelimit.TokenSource
	Logger                  *slog.Logger
	MailtrapToken           string
	MailtrapTimeout         time.Duration
	MailtrapVerifyTolerance time.Duration
	SweegoToken             string
	SweegoProvider          string
	SweegoBaseURL           string
	SweegoTimeout           time.Duration
	SweegoVerifyTolerance   time.Duration
}

// Plugin adapts durable email delivery to the last_orders application.
type Plugin struct {
	store    *durableemail.Store
	client   clients.EmailClient
	verifier clients.EmailVerifier
	tokens   ratelimit.TokenSource
	logger   *slog.Logger
}

func New(db *sql.DB, opts Options) (*Plugin, error) {
	if opts.Tokens == nil {
		return nil, fmt.Errorf("email send token source is required")
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	var client clients.EmailClient
	var verifier clients.EmailVerifier
	mailtrapToken := strings.TrimSpace(opts.MailtrapToken)
	sweegoToken := strings.TrimSpace(opts.SweegoToken)
	if mailtrapToken != "" && sweegoToken != "" {
		return nil, fmt.Errorf("tokens for Mailtrap and Sweego are mutually exclusive")
	}
	switch opts.Client {
	case ClientDummy:
		dummyClient := newDummyClient(logger)
		client, verifier = dummyClient, dummyClient
	case ClientMailtrap:
		if mailtrapToken == "" {
			return nil, fmt.Errorf("a token is required for Mailtrap")
		}
		timeout := opts.MailtrapTimeout
		if timeout <= 0 {
			timeout = 30 * time.Second
		}
		sdkClient, err := sdk.NewClient(mailtrapToken, sdk.WithHTTPClient(&http.Client{Timeout: timeout}))
		if err != nil {
			return nil, fmt.Errorf("init Mailtrap client: %w", err)
		}
		mailtrapClient, err := mailtrap.NewClient(sdkClient)
		if err != nil {
			return nil, fmt.Errorf("init Mailtrap client: %w", err)
		}
		tolerance := opts.MailtrapVerifyTolerance
		if tolerance <= 0 {
			tolerance = 5 * time.Minute
		}
		client, verifier = mailtrapClient, mailtrap.NewVerifier(mailtrapClient, tolerance)
	case ClientSweego:
		token := sweegoToken
		provider := strings.TrimSpace(opts.SweegoProvider)
		if token == "" || provider == "" {
			return nil, fmt.Errorf("both token and provider are required for Sweego")
		}
		baseURL := strings.TrimRight(strings.TrimSpace(opts.SweegoBaseURL), "/")
		if baseURL == "" {
			baseURL = "https://api.sweego.io"
		}
		timeout := opts.SweegoTimeout
		if timeout <= 0 {
			timeout = 30 * time.Second
		}
		sweegoClient := sweego.NewClient(baseURL, token, timeout).WithSendOptions(sweego.SendOptions{Provider: provider})
		tolerance := opts.SweegoVerifyTolerance
		if tolerance <= 0 {
			tolerance = 5 * time.Minute
		}
		client, verifier = sweegoClient, sweegologs.NewVerifier(sweegologs.NewClient(sweegoClient), tolerance)
	case "":
		return nil, fmt.Errorf("email client kind is required")
	default:
		return nil, fmt.Errorf("unknown email client kind %q", opts.Client)
	}

	store, err := durableemail.NewStore(db)
	if err != nil {
		return nil, fmt.Errorf("init durable email store: %w", err)
	}
	return &Plugin{store: store, client: client, verifier: verifier, tokens: opts.Tokens, logger: logger}, nil
}

func (p *Plugin) Register(runtime *cellar.Cellar) error {
	return durableemail.Register(runtime, p.store, p.client, p.verifier, durableemail.RegisterOptions{SubmissionGuard: p.guardSubmission})
}

func (p *Plugin) guardSubmission(_ context.Context, submission durableemail.Submission) (time.Duration, error) {
	switch {
	case strings.HasPrefix(submission.IdempotencyToken, "test-email:"):
		return 0, nil
	case strings.HasPrefix(submission.IdempotencyToken, "poll-opened:"), strings.HasPrefix(submission.IdempotencyToken, "poll-completed:"):
		wait, err := p.tokens.AcquireN(submission.RecipientCount)
		var oversized *ratelimit.RequestExceedsCapacityError
		if errors.As(err, &oversized) {
			p.logger.Error("email batch exceeds daily capacity; deferring for 24 hours",
				"source", "email.send", "idempotency_token", submission.IdempotencyToken,
				"recipient_count", submission.RecipientCount, "maximum", oversized.Maximum, "err", err)
			return 24 * time.Hour, nil
		}
		if err != nil {
			return 0, err
		}
		if wait < 0 {
			return 0, fmt.Errorf("email send token source returned a negative wait")
		}
		return time.Duration(wait) * time.Second, nil
	default:
		return 0, fmt.Errorf("email request %q has no rate limit policy", submission.IdempotencyToken)
	}
}

func (p *Plugin) QueryProgress(ctx context.Context, filter durableemail.ProgressFilter) ([]durableemail.ProgressRow, error) {
	return p.store.QueryProgress(ctx, filter)
}

// RecoverSubmissions must run before Cellar starts, while no email workers exist.
func (p *Plugin) RecoverSubmissions(ctx context.Context) error {
	return p.store.RecoverSubmissions(ctx)
}

func newDummyClient(logger *slog.Logger) *dummy.Client {
	client := dummy.NewClient(func(emailAddress, message string, headers map[string]string) (dummy.Response, error) {
		logger.Info("dummy email sent",
			"recipient", emailAddress,
			"message_id", headers[durableemail.MessageIDHeader],
			"message", message,
		)
		return dummy.Response{}, nil
	})
	// Verification can only find sends this process has recorded.
	client.OnAccepted(client.RecordAccepted)
	return client
}
