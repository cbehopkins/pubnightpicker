package dummy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"email_clients/clients"
	"email_clients/internal/texttemplate"
)

var (
	ErrNilCallback       = errors.New("dummy email client callback is nil")
	ErrTemplateExists    = errors.New("dummy email template already exists")
	ErrTemplateNameEmpty = errors.New("dummy email template name is empty")
	ErrTemplateNotFound  = errors.New("dummy email template not found")
	ErrNoRecipients      = errors.New("email has no recipients")
)

// Response is the simulated provider's answer for one recipient. The zero
// value is an acceptance, so a callback that models a healthy service can
// return Response{}. A Status outside 2xx (other than the unset 0) is a
// refusal, mirroring a real provider answering with a non-2xx status.
type Response struct {
	Status int
	Body   string
	PMUID  string
}

// SendCallback models the provider call for a single recipient. A non-nil
// error is a transport fault (no answer); a refusing Response is an answer.
type SendCallback func(emailAddress, message string, headers map[string]string) (Response, error)

// RefusedError reports that the simulated service answered and declined the
// recipient.
type RefusedError struct {
	Recipient string
	Status    int
	Body      string
}

func (e *RefusedError) Error() string {
	body := strings.TrimSpace(e.Body)
	if body == "" {
		return fmt.Sprintf("dummy email service refused %q: HTTP %d", e.Recipient, e.Status)
	}
	return fmt.Sprintf("dummy email service refused %q: HTTP %d: %s", e.Recipient, e.Status, body)
}

func (r Response) refused() bool {
	return r.Status != 0 && (r.Status < 200 || r.Status >= 300)
}

// Accepted describes one recipient the simulated service accepted.
type Accepted struct {
	CorrelationID string
	Recipient     string
	PMUID         string
	Message       string
	Headers       map[string]string
}

// AcceptedHook runs once per accepted recipient, after Send has assigned its
// PMUID. Hooks run synchronously inside Send; use Delayed for later work.
type AcceptedHook func(Accepted)

// Delayed returns a hook that runs hook on a new goroutine after d, modelling
// provider-side work that lands some time after the request was accepted.
func Delayed(d time.Duration, hook AcceptedHook) AcceptedHook {
	return func(accepted Accepted) {
		go func() {
			time.Sleep(d)
			hook(accepted)
		}()
	}
}

type callbackMessage struct {
	emailAddress string
	message      string
	headers      map[string]string
}

type Client struct {
	callback SendCallback

	hooksMu sync.Mutex
	hooks   []AcceptedHook

	templatesMu sync.RWMutex
	templates   map[string]string

	recordsMu sync.Mutex
	records   []sentRecord
}

// sentRecord is the dummy client's local record of a recipient the callback
// accepted, used to answer later Verify calls without a real provider.
type sentRecord struct {
	correlationID string
	recipient     string
	pmuid         string
}

var _ clients.EmailClient = (*Client)(nil)
var _ clients.EmailVerifier = (*Client)(nil)

func NewClient(callback SendCallback) *Client {
	return &Client{callback: callback, templates: make(map[string]string)}
}

// OnAccepted appends hook to the hooks run, in registration order, for each
// accepted recipient. A nil hook is ignored.
func (c *Client) OnAccepted(hook AcceptedHook) {
	if hook == nil {
		return
	}
	c.hooksMu.Lock()
	defer c.hooksMu.Unlock()
	c.hooks = append(c.hooks, hook)
}

// RecordAcceptedAfter registers RecordAccepted to run d after each acceptance,
// modelling the provider's log ingestion lag.
func (c *Client) RecordAcceptedAfter(d time.Duration) {
	c.OnAccepted(Delayed(d, c.RecordAccepted))
}

func (c *Client) AddTemplate(name, source string) error {
	if strings.TrimSpace(name) == "" {
		return ErrTemplateNameEmpty
	}

	if err := texttemplate.Validate(source); err != nil {
		return fmt.Errorf("parse dummy email template %q: %w", name, err)
	}

	c.templatesMu.Lock()
	defer c.templatesMu.Unlock()
	if _, exists := c.templates[name]; exists {
		return fmt.Errorf("%w: %q", ErrTemplateExists, name)
	}
	if c.templates == nil {
		c.templates = make(map[string]string)
	}
	c.templates[name] = source
	return nil
}

func (c *Client) Send(ctx context.Context, email clients.Email) (clients.SendResult, error) {
	if c.callback == nil {
		return clients.SendResult{}, ErrNilCallback
	}
	if len(email.To) == 0 {
		return clients.SendResult{}, ErrNoRecipients
	}

	messages := make([]callbackMessage, 0, len(email.To))
	for _, recipient := range email.To {
		if err := ctx.Err(); err != nil {
			return clients.SendResult{}, err
		}
		message, err := c.render(email.TemplateID, email.Text, mergedVariables(email.Variables, recipient.Variables))
		if err != nil {
			return clients.SendResult{}, fmt.Errorf("render email for %q: %w", recipient.Email, err)
		}
		messages = append(messages, callbackMessage{
			emailAddress: recipient.Email,
			message:      message,
			headers:      email.Headers,
		})
	}
	return c.send(ctx, messages)
}

func mergedVariables(common, recipient map[string]any) map[string]any {
	variables := maps.Clone(common)
	if variables == nil && len(recipient) != 0 {
		variables = make(map[string]any, len(recipient))
	}
	maps.Copy(variables, recipient)
	return variables
}

func (c *Client) render(templateID, message string, variables map[string]any) (string, error) {
	source := message
	if templateID != "" {
		c.templatesMu.RLock()
		registered, ok := c.templates[templateID]
		c.templatesMu.RUnlock()
		if !ok {
			return "", fmt.Errorf("%w: %q", ErrTemplateNotFound, templateID)
		}
		source = registered
	}
	return texttemplate.Render(source, variables)
}

func (c *Client) send(ctx context.Context, messages []callbackMessage) (clients.SendResult, error) {
	c.hooksMu.Lock()
	hooks := slices.Clone(c.hooks)
	c.hooksMu.Unlock()

	result := clients.SendResult{Recipients: make([]clients.RecipientResult, len(messages))}
	for index, message := range messages {
		if err := ctx.Err(); err != nil {
			return clients.SendResult{}, err
		}
		response, err := c.callback(message.emailAddress, message.message, cloneHeaders(message.headers))
		if err != nil {
			return clients.SendResult{}, fmt.Errorf("send email to %q: %w", message.emailAddress, err)
		}
		if response.refused() {
			return clients.SendResult{}, &RefusedError{Recipient: message.emailAddress, Status: response.Status, Body: response.Body}
		}
		pmuid := response.PMUID
		if pmuid == "" {
			generated, err := newPMUID()
			if err != nil {
				return clients.SendResult{}, err
			}
			pmuid = generated
		}
		result.Recipients[index].PMUID = pmuid
		for _, hook := range hooks {
			hook(Accepted{
				CorrelationID: message.headers[clients.CorrelationHeader],
				Recipient:     message.emailAddress,
				PMUID:         pmuid,
				Message:       message.message,
				Headers:       cloneHeaders(message.headers),
			})
		}
	}

	return result, nil
}

// RecordAccepted stores accepted so Verify can find it. Register it with
// OnAccepted for immediate records, or use RecordAcceptedAfter.
func (c *Client) RecordAccepted(accepted Accepted) {
	c.recordsMu.Lock()
	defer c.recordsMu.Unlock()
	c.records = append(c.records, sentRecord{correlationID: accepted.CorrelationID, recipient: accepted.Recipient, pmuid: accepted.PMUID})
}

// Verify requires an exact, non-empty correlation ID match, mirroring the
// strict single-message contract used by provider verifiers rather than the
// looser multi-candidate matching used by bulk recovery.
func (c *Client) Verify(ctx context.Context, request clients.VerifyRequest) (clients.VerifyResult, error) {
	if err := ctx.Err(); err != nil {
		return clients.VerifyResult{}, err
	}

	c.recordsMu.Lock()
	defer c.recordsMu.Unlock()
	for _, record := range c.records {
		if record.correlationID == "" || record.correlationID != request.CorrelationID {
			continue
		}
		if !strings.EqualFold(record.recipient, request.Recipient) {
			continue
		}
		return clients.VerifyResult{Found: true, PMUID: record.pmuid}, nil
	}
	return clients.VerifyResult{}, nil
}

func newPMUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate dummy PMUID: %w", err)
	}
	return "dummy-" + hex.EncodeToString(value[:]), nil
}

func cloneHeaders(headers map[string]string) map[string]string {
	if headers == nil {
		return nil
	}

	clone := make(map[string]string, len(headers))
	maps.Copy(clone, headers)
	return clone
}
