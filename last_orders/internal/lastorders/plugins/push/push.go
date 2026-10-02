// Package push delivers Web Push notifications durably, one Cellar Cell per endpoint.
// See docs/cdd/0008-push-notifications.md.
package push

import (
	"context"
	"crypto/ecdh"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"cellar/pkg/cellar"
	"last_orders/internal/lastorders/components/notificationprofile"
	"last_orders/internal/lastorders/components/ratelimit"

	webpush "github.com/SherClockHolmes/webpush-go"
)

const (
	HandlerDeliver cellar.HandlerName = "push.deliver"
	HandlerWait    cellar.HandlerName = "push.wait"

	DefaultBaseURL = "https://ampubnight.org"

	waitDelay     = 5 * time.Second
	minRetryDelay = 30 * time.Second
	maxRetryDelay = time.Hour
	minTTL        = time.Hour
	maxTTL        = 5 * 24 * time.Hour
	topicMaxLen   = 32
)

// Delivery states for one notification to one endpoint.
const (
	StatePending  = "Pending"
	StateAccepted = "Accepted"
	StateRejected = "Rejected"
	StateExpired  = "Expired"
)

// ClientKind selects how push messages leave the process.
type ClientKind string

const (
	// ClientDummy logs each push instead of sending it.
	ClientDummy   ClientKind = "dummy"
	ClientWebPush ClientKind = "webpush"
)

type Options struct {
	Client ClientKind
	Tokens ratelimit.TokenSource
	// VAPIDPrivateKey is the base64url P-256 private key from `npx web-push generate-vapid-keys`.
	VAPIDPrivateKey string
	// VAPIDSubject is an email address, mailto: URI, or https URL.
	VAPIDSubject string
	BaseURL      string
	Logger       *slog.Logger
}

type Subscription struct {
	Endpoint string
	P256DH   string
	Auth     string
}

// Sender performs one Web Push request and reports the push service's HTTP status.
type Sender interface {
	Send(ctx context.Context, subscription Subscription, message []byte, ttl time.Duration, topic string) (int, error)
}

// EndpointInvalidator deactivates an endpoint in the authoritative endpoint store.
type EndpointInvalidator interface {
	InvalidateEndpoint(ctx context.Context, userID, endpointID string) error
}

type Plugin struct {
	db          *sql.DB
	sender      Sender
	invalidator EndpointInvalidator
	baseURL     string
	logger      *slog.Logger
	tokens      ratelimit.TokenSource
}

func New(db *sql.DB, invalidator EndpointInvalidator, opts Options) (*Plugin, error) {
	if db == nil {
		return nil, errors.New("db is required")
	}
	if invalidator == nil {
		return nil, errors.New("endpoint invalidator is required")
	}
	if opts.Tokens == nil {
		return nil, errors.New("push send token source is required")
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	var sender Sender
	switch opts.Client {
	case ClientDummy:
		sender = dummySender{logger: logger}
	case ClientWebPush:
		var err error
		sender, err = newWebPushSender(opts.VAPIDPrivateKey, opts.VAPIDSubject)
		if err != nil {
			return nil, err
		}
	case "":
		return nil, errors.New("push client kind is required")
	default:
		return nil, fmt.Errorf("unknown push client kind %q", opts.Client)
	}

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS push_deliveries (
			notification_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			endpoint_id TEXT NOT NULL,
			state TEXT NOT NULL,
			attempts INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (notification_id, user_id, endpoint_id)
		);
	`); err != nil {
		return nil, fmt.Errorf("create push_deliveries schema: %w", err)
	}

	baseURL := strings.TrimRight(opts.BaseURL, "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Plugin{db: db, sender: sender, invalidator: invalidator, baseURL: baseURL, logger: logger, tokens: opts.Tokens}, nil
}

// BaseURL is the web application origin used for notification click targets.
func (p *Plugin) BaseURL() string {
	return p.baseURL
}

// Accepted counts the push service acceptances recorded for notificationID.
func (p *Plugin) Accepted(ctx context.Context, notificationID string) (int, error) {
	var accepted int
	err := p.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM push_deliveries WHERE notification_id = ? AND state = ?
	`, notificationID, StateAccepted).Scan(&accepted)
	return accepted, err
}

func (p *Plugin) Register(runtime *cellar.Cellar) error {
	if err := runtime.Register(HandlerDeliver, deliverHandler{plugin: p}); err != nil {
		return err
	}
	return runtime.Register(HandlerWait, waitHandler{plugin: p})
}

// Notification is one push message with a stable identity.
type Notification struct {
	ID        string
	Message   []byte
	Topic     string
	ExpiresAt time.Time
}

type deliveryRequest struct {
	NotificationID string    `json:"notification_id"`
	UserID         string    `json:"user_id"`
	EndpointID     string    `json:"endpoint_id"`
	Endpoint       string    `json:"endpoint"`
	P256DH         string    `json:"p256dh"`
	Auth           string    `json:"auth"`
	Message        string    `json:"message"`
	Topic          string    `json:"topic"`
	ExpiresAt      time.Time `json:"expires_at"`
}

type waitRequest struct {
	NotificationID string `json:"notification_id"`
}

// Populate commits the endpoint population for notification together with one
// delivery Cell per endpoint. then runs once every delivery is terminal.
func (p *Plugin) Populate(ctx context.Context, notification Notification, endpoints []notificationprofile.Endpoint, then ...cellar.Step) (cellar.Complete, error) {
	var existing int
	if err := p.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM push_deliveries WHERE notification_id = ?
	`, notification.ID).Scan(&existing); err != nil {
		return cellar.Complete{}, fmt.Errorf("read push population: %w", err)
	}
	if existing > 0 {
		return cellar.Complete{}, nil
	}

	cells := make([]cellar.CellRequest, 0, len(endpoints)+1)
	for _, endpoint := range endpoints {
		cell, err := cellRequest(cellar.Step{HandlerName: HandlerDeliver, Payload: deliveryRequest{
			NotificationID: notification.ID,
			UserID:         endpoint.UserID,
			EndpointID:     endpoint.EndpointID,
			Endpoint:       endpoint.URL,
			P256DH:         endpoint.P256DH,
			Auth:           endpoint.Auth,
			Message:        string(notification.Message),
			Topic:          notification.Topic,
			ExpiresAt:      notification.ExpiresAt,
		}})
		if err != nil {
			return cellar.Complete{}, err
		}
		cells = append(cells, cell)
	}

	steps := then
	if len(endpoints) > 0 {
		steps = append([]cellar.Step{{HandlerName: HandlerWait, Payload: waitRequest{NotificationID: notification.ID}}}, then...)
	}
	if len(steps) > 0 {
		cell, err := cellRequest(steps...)
		if err != nil {
			return cellar.Complete{}, err
		}
		cells = append(cells, cell)
	}
	return cellar.Complete{NewCells: cells, ApplicationWork: []cellar.ApplicationWork{populationWork(notification.ID, endpoints)}}, nil
}

func cellRequest(steps ...cellar.Step) (cellar.CellRequest, error) {
	sequence, err := cellar.NewSequence(steps...)
	if err != nil {
		return cellar.CellRequest{}, err
	}
	return sequence.CellRequest()
}

func decode(payload []byte, target any) error {
	return json.Unmarshal(payload, target)
}

func populationWork(notificationID string, endpoints []notificationprofile.Endpoint) cellar.ApplicationWork {
	return func(tx cellar.ApplicationTx) error {
		for _, endpoint := range endpoints {
			if err := tx.Exec(`
				INSERT INTO push_deliveries (notification_id, user_id, endpoint_id, state)
				VALUES (?, ?, ?, ?)
				ON CONFLICT DO NOTHING
			`, notificationID, endpoint.UserID, endpoint.EndpointID, StatePending); err != nil {
				return fmt.Errorf("insert push delivery: %w", err)
			}
		}
		return nil
	}
}

func finishWork(request deliveryRequest, state string) cellar.ApplicationWork {
	return func(tx cellar.ApplicationTx) error {
		return tx.Exec(`
			UPDATE push_deliveries SET state = ?
			WHERE notification_id = ? AND user_id = ? AND endpoint_id = ? AND state = ?
		`, state, request.NotificationID, request.UserID, request.EndpointID, StatePending)
	}
}

func attemptWork(request deliveryRequest) cellar.ApplicationWork {
	return func(tx cellar.ApplicationTx) error {
		return tx.Exec(`
			UPDATE push_deliveries SET attempts = attempts + 1
			WHERE notification_id = ? AND user_id = ? AND endpoint_id = ?
		`, request.NotificationID, request.UserID, request.EndpointID)
	}
}

type deliverHandler struct {
	plugin *Plugin
}

func (h deliverHandler) Handle(ctx context.Context, request deliveryRequest) cellar.Result {
	var state string
	var attempts int
	err := h.plugin.db.QueryRowContext(ctx, `
		SELECT state, attempts FROM push_deliveries
		WHERE notification_id = ? AND user_id = ? AND endpoint_id = ?
	`, request.NotificationID, request.UserID, request.EndpointID).Scan(&state, &attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return cellar.Complete{}
	}
	if err != nil {
		return cellar.ErrorResult{Message: "read push delivery", Err: err}
	}
	if state != StatePending {
		return cellar.Complete{}
	}

	remaining := time.Until(request.ExpiresAt)
	if remaining <= 0 {
		h.plugin.logger.Warn("push expired before delivery", "notification_id", request.NotificationID, "user_id", request.UserID)
		return cellar.Complete{ApplicationWork: []cellar.ApplicationWork{finishWork(request, StateExpired)}}
	}

	if wait := h.plugin.tokens.Acquire(); wait != 0 {
		if wait < 0 {
			return cellar.ErrorResult{Message: "push send token source returned a negative wait"}
		}
		notBefore := time.Now().UTC().Add(time.Duration(wait) * time.Second)
		if notBefore.After(request.ExpiresAt) {
			notBefore = request.ExpiresAt
		}
		return cellar.Retry{NotBefore: &notBefore}
	}

	subscription := Subscription{Endpoint: request.Endpoint, P256DH: request.P256DH, Auth: request.Auth}
	status, err := h.plugin.sender.Send(ctx, subscription, []byte(request.Message), remaining, request.Topic)
	switch {
	case err == nil && status >= 200 && status < 300:
		return cellar.Complete{ApplicationWork: []cellar.ApplicationWork{finishWork(request, StateAccepted)}}
	case err == nil && permanentFailure(status):
		if err := h.plugin.invalidator.InvalidateEndpoint(ctx, request.UserID, request.EndpointID); err != nil {
			h.plugin.logger.Error("invalidate push endpoint failed", "user_id", request.UserID, "endpoint_id", request.EndpointID, "err", err)
			return h.retry(request, attempts)
		}
		h.plugin.logger.Info("push endpoint invalidated", "user_id", request.UserID, "endpoint_id", request.EndpointID, "status", status)
		return cellar.Complete{ApplicationWork: []cellar.ApplicationWork{finishWork(request, StateRejected)}}
	default:
		h.plugin.logger.Warn("transient push failure", "user_id", request.UserID, "endpoint_id", request.EndpointID, "status", status, "err", err)
		return h.retry(request, attempts)
	}
}

func (h deliverHandler) retry(request deliveryRequest, attempts int) cellar.Result {
	delay := maxRetryDelay
	if attempts < 7 {
		delay = min(minRetryDelay<<attempts, maxRetryDelay)
	}
	notBefore := time.Now().UTC().Add(delay)
	if notBefore.After(request.ExpiresAt) {
		notBefore = request.ExpiresAt
	}
	return cellar.Retry{NotBefore: &notBefore, ApplicationWork: []cellar.ApplicationWork{attemptWork(request)}}
}

// permanentFailure mirrors the statuses on which the Python notifier deactivates endpoints.
func permanentFailure(status int) bool {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusGone:
		return true
	default:
		return false
	}
}

type waitHandler struct {
	plugin *Plugin
}

func (h waitHandler) Handle(ctx context.Context, request waitRequest) cellar.Result {
	var pending int
	if err := h.plugin.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM push_deliveries WHERE notification_id = ? AND state = ?
	`, request.NotificationID, StatePending).Scan(&pending); err != nil {
		return cellar.ErrorResult{Message: "read pending push deliveries", Err: err}
	}
	if pending > 0 {
		notBefore := time.Now().UTC().Add(waitDelay)
		return cellar.Retry{NotBefore: &notBefore}
	}
	return cellar.Complete{}
}

// TTL mirrors Python's policy: midnight UTC on the event day, clamped to [1h, 5d].
func TTL(pollDate string, now time.Time) time.Duration {
	eventDay, err := time.Parse(time.DateOnly, pollDate)
	if err != nil {
		return minTTL
	}
	return min(max(eventDay.Sub(now), minTTL), maxTTL)
}

// Topic mirrors Python's _topic_for_poll_id: at most 32 URL-safe characters.
func Topic(id string) string {
	sanitised := strings.Map(func(r rune) rune {
		if r < 128 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return r
		}
		return '-'
	}, id)
	if sanitised == "" {
		return digest(id)[:topicMaxLen]
	}
	if len(sanitised) <= topicMaxLen {
		return sanitised
	}
	return sanitised[:topicMaxLen-13] + "-" + digest(sanitised)[:12]
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

type dummySender struct {
	logger *slog.Logger
}

func (s dummySender) Send(_ context.Context, subscription Subscription, message []byte, ttl time.Duration, topic string) (int, error) {
	s.logger.Info("dummy push sent", "endpoint", subscription.Endpoint, "topic", topic, "ttl_seconds", int(ttl.Seconds()), "message", string(message))
	return http.StatusCreated, nil
}

type webPushSender struct {
	privateKey string
	publicKey  string
	subscriber string
}

func newWebPushSender(privateKey, subject string) (*webPushSender, error) {
	if privateKey == "" {
		return nil, errors.New("VAPID private key is required for web push")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(privateKey, "="))
	if err != nil {
		return nil, fmt.Errorf("decode VAPID private key: %w", err)
	}
	key, err := ecdh.P256().NewPrivateKey(raw)
	if err != nil {
		return nil, fmt.Errorf("parse VAPID private key: %w", err)
	}
	subscriber, err := vapidSubscriber(subject)
	if err != nil {
		return nil, err
	}
	return &webPushSender{
		privateKey: privateKey,
		publicKey:  base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()),
		subscriber: subscriber,
	}, nil
}

// vapidSubscriber accepts Python's subject forms; webpush-go adds mailto: itself.
func vapidSubscriber(subject string) (string, error) {
	subject = strings.TrimSpace(subject)
	switch {
	case strings.HasPrefix(subject, "https://"):
		return subject, nil
	case strings.HasPrefix(subject, "mailto:"):
		subject = strings.TrimPrefix(subject, "mailto:")
	}
	if !strings.Contains(subject, "@") {
		return "", errors.New("VAPID subject must be an email address, mailto: URI, or https URL")
	}
	return subject, nil
}

func (s *webPushSender) Send(ctx context.Context, subscription Subscription, message []byte, ttl time.Duration, topic string) (int, error) {
	response, err := webpush.SendNotificationWithContext(ctx, message, &webpush.Subscription{
		Endpoint: subscription.Endpoint,
		Keys:     webpush.Keys{P256dh: subscription.P256DH, Auth: subscription.Auth},
	}, &webpush.Options{
		Subscriber:      s.subscriber,
		VAPIDPublicKey:  s.publicKey,
		VAPIDPrivateKey: s.privateKey,
		TTL:             max(int(ttl.Seconds()), 1),
		Topic:           topic,
	})
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	return response.StatusCode, nil
}
