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
	"last_orders/internal/lastorders/components/diagnosticsconfig"
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
	NotificationSettings    func() (diagnosticsconfig.Settings, bool)
	MailtrapSandboxToken    string
	MailtrapSandboxID       int64
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
	clientKind           ClientKind
	store                *durableemail.Store
	client               clients.EmailClient
	suppressedClient     clients.EmailClient
	notificationSettings func() (diagnosticsconfig.Settings, bool)
	verifier             clients.EmailVerifier
	tokens               ratelimit.TokenSource
	logger               *slog.Logger
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
	var suppressedClient clients.EmailClient
	var verifier clients.EmailVerifier
	mailtrapToken := strings.TrimSpace(opts.MailtrapToken)
	sweegoToken := strings.TrimSpace(opts.SweegoToken)
	if mailtrapToken != "" && sweegoToken != "" {
		return nil, fmt.Errorf("tokens for Mailtrap and Sweego are mutually exclusive")
	}
	switch opts.Client {
	case ClientDummy:
		dummyClient := newDummyClient(logger)
		dummyClient.SetDryRunCallback(func() bool { return false })
		client, verifier = dummyClient, dummyClient
		suppressedDummy := newDummyClient(logger)
		suppressedDummy.SetDryRunCallback(func() bool { return true })
		suppressedClient = suppressedDummy
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
		suppressedMailtrap, err := mailtrap.NewClient(sdkClient)
		if err != nil {
			return nil, err
		}
		if opts.MailtrapSandboxID < 0 {
			return nil, fmt.Errorf("sandbox ID for Mailtrap must be positive")
		}
		if opts.MailtrapSandboxID > 0 {
			token := strings.TrimSpace(opts.MailtrapSandboxToken)
			if token == "" {
				token = mailtrapToken
			}
			sandboxSDK, err := sdk.NewClient(token, sdk.WithSandbox(true), sdk.WithSandboxID(opts.MailtrapSandboxID), sdk.WithHTTPClient(&http.Client{Timeout: timeout}))
			if err != nil {
				return nil, fmt.Errorf("init Mailtrap sandbox: %w", err)
			}
			suppressedMailtrap, err = suppressedMailtrap.WithSandboxClient(sandboxSDK)
			if err != nil {
				return nil, err
			}
		}
		suppressedMailtrap.SetDryRunCallback(func() bool { return true })
		suppressedClient = suppressedMailtrap
		mailtrapClient, err := mailtrap.NewClient(sdkClient)
		if err != nil {
			return nil, fmt.Errorf("init Mailtrap client: %w", err)
		}
		mailtrapClient.SetDryRunCallback(func() bool { return false })
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
		sweegoClient.SetDryRunCallback(func() bool { return false })
		suppressedSweego := sweego.NewClient(baseURL, token, timeout).WithSendOptions(sweego.SendOptions{Provider: provider})
		suppressedSweego.SetDryRunCallback(func() bool { return true })
		suppressedClient = suppressedSweego
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
	logger.Info("email plugin configured", "client", opts.Client, "sandbox_configured", opts.Client == ClientMailtrap && opts.MailtrapSandboxID > 0)
	return &Plugin{clientKind: opts.Client, store: store, client: client, suppressedClient: suppressedClient, notificationSettings: opts.NotificationSettings, verifier: verifier, tokens: opts.Tokens, logger: logger}, nil
}

func (p *Plugin) Register(runtime *cellar.Cellar) error {
	return durableemail.Register(runtime, p.store, p.client, p.verifier, durableemail.RegisterOptions{SubmissionGuard: p.guardSubmission, SubmissionPolicy: p.submissionPolicy, Logger: p.logger})
}

func (p *Plugin) submissionPolicy(_ context.Context, submission durableemail.Submission) (durableemail.SubmissionDecision, error) {
	var settings diagnosticsconfig.Settings
	if p.notificationSettings != nil {
		var known bool
		settings, known = p.notificationSettings()
		if !known {
			return durableemail.SubmissionDecision{Delay: time.Second}, nil
		}
	}
	actorException := settings.SilenceNotifications && submission.RecipientCount == 1 &&
		(submission.Metadata["purpose"] == diagnosticsconfig.PurposePollOpened || submission.Metadata["purpose"] == diagnosticsconfig.PurposePollCompleted) &&
		settings.AllowsLive(submission.Metadata["purpose"], submission.Metadata["actor_uid"], submission.Metadata["recipient_uid"])
	decision := durableemail.SubmissionDecision{Client: p.client}
	mode := "live"
	if p.clientKind == ClientDummy {
		mode = "dummy"
	}
	if settings.SilenceNotifications && !actorException {
		decision = durableemail.SubmissionDecision{Client: p.suppressedClient, BestEffort: true}
		switch p.clientKind {
		case ClientMailtrap:
			mode = "sandbox"
		case ClientSweego:
			mode = "dry-run"
		case ClientDummy:
			mode = "dummy-dry-run"
		}
	}
	p.logger.Info("email submission selected", "client", p.clientKind, "mode", mode, "actor_exception", actorException, "idempotency_token", submission.IdempotencyToken, "recipient_count", submission.RecipientCount)
	return decision, nil
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
		dryRun := headers[clients.DryRunHeader] == "true"
		logMessage := "dummy email sent"
		if dryRun {
			logMessage = "dummy email suppressed (dry-run)"
		}
		logger.Info(logMessage,
			"dry_run", dryRun,
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
