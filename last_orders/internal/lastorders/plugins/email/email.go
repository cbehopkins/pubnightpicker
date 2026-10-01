package email

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"cellar/pkg/cellar"
	durableemail "durable_email"
	"email_clients/clients"
	"email_clients/clients/dummy"
	"email_clients/clients/sweego"
	sweegologs "email_clients/clients/sweego/logs"
)

// ClientKind selects the email provider the plugin sends through.
type ClientKind string

const (
	// ClientDummy logs each email instead of sending it.
	ClientDummy ClientKind = "dummy"
	// ClientSweego sends email through Sweego and verifies delivery through its logs API.
	ClientSweego ClientKind = "sweego"
)

type Options struct {
	Client                ClientKind
	Logger                *slog.Logger
	SweegoToken           string
	SweegoProvider        string
	SweegoBaseURL         string
	SweegoTimeout         time.Duration
	SweegoVerifyTolerance time.Duration
}

// Plugin adapts durable email delivery to the last_orders application.
type Plugin struct {
	store    *durableemail.Store
	client   clients.EmailClient
	verifier clients.EmailVerifier
}

func New(db *sql.DB, opts Options) (*Plugin, error) {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	var client clients.EmailClient
	var verifier clients.EmailVerifier
	switch opts.Client {
	case ClientDummy:
		dummyClient := newDummyClient(logger)
		client, verifier = dummyClient, dummyClient
	case ClientSweego:
		token := strings.TrimSpace(opts.SweegoToken)
		provider := strings.TrimSpace(opts.SweegoProvider)
		if token == "" || provider == "" {
			return nil, fmt.Errorf("Sweego requires both token and provider")
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
	return &Plugin{store: store, client: client, verifier: verifier}, nil
}

func (p *Plugin) Register(runtime *cellar.Cellar) error {
	return durableemail.Register(runtime, p.store, p.client, p.verifier)
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
