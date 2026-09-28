package dummy

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"
	"text/template"

	"email_clients/clients"
)

var (
	ErrNilCallback       = errors.New("dummy email client callback is nil")
	ErrTemplateExists    = errors.New("dummy email template already exists")
	ErrTemplateNameEmpty = errors.New("dummy email template name is empty")
	ErrTemplateNotFound  = errors.New("dummy email template not found")
	ErrNoRecipients      = errors.New("email has no recipients")
)

type SendCallback func(emailAddress, message string, headers map[string]string) error

type callbackMessage struct {
	emailAddress string
	message      string
	headers      map[string]string
}

type Client struct {
	callback SendCallback

	templatesMu sync.RWMutex
	templates   map[string]*template.Template

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
	return &Client{callback: callback, templates: make(map[string]*template.Template)}
}

func (c *Client) AddTemplate(name, source string) error {
	if strings.TrimSpace(name) == "" {
		return ErrTemplateNameEmpty
	}

	parsed, err := template.New(name).Option("missingkey=error").Parse(source)
	if err != nil {
		return fmt.Errorf("parse dummy email template %q: %w", name, err)
	}

	c.templatesMu.Lock()
	defer c.templatesMu.Unlock()
	if _, exists := c.templates[name]; exists {
		return fmt.Errorf("%w: %q", ErrTemplateExists, name)
	}
	if c.templates == nil {
		c.templates = make(map[string]*template.Template)
	}
	c.templates[name] = parsed
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
	if templateID == "" {
		return message, nil
	}

	c.templatesMu.RLock()
	registered := c.templates[templateID]
	c.templatesMu.RUnlock()
	if registered == nil {
		return "", fmt.Errorf("%w: %q", ErrTemplateNotFound, templateID)
	}

	var rendered bytes.Buffer
	if err := registered.Execute(&rendered, variables); err != nil {
		return "", fmt.Errorf("execute dummy email template %q: %w", templateID, err)
	}
	return rendered.String(), nil
}

func (c *Client) send(ctx context.Context, messages []callbackMessage) (clients.SendResult, error) {
	result := clients.SendResult{Recipients: make([]clients.RecipientResult, len(messages))}
	for index, message := range messages {
		if err := ctx.Err(); err != nil {
			return clients.SendResult{}, err
		}
		if err := c.callback(message.emailAddress, message.message, cloneHeaders(message.headers)); err != nil {
			return clients.SendResult{}, fmt.Errorf("send email to %q: %w", message.emailAddress, err)
		}
		pmuid, err := newPMUID()
		if err != nil {
			return clients.SendResult{}, err
		}
		result.Recipients[index].PMUID = pmuid
		c.record(message.headers[clients.CorrelationHeader], message.emailAddress, pmuid)
	}

	return result, nil
}

func (c *Client) record(correlationID, recipient, pmuid string) {
	c.recordsMu.Lock()
	defer c.recordsMu.Unlock()
	c.records = append(c.records, sentRecord{correlationID: correlationID, recipient: recipient, pmuid: pmuid})
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
