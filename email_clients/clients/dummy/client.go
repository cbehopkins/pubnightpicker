package dummy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
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
}

var _ clients.EmailClient = (*Client)(nil)

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

func (c *Client) SendEmail(ctx context.Context, req clients.SendEmailRequest) (clients.HTTPResult, error) {
	messages := make([]callbackMessage, 0, len(req.Recipients))
	for _, recipient := range req.Recipients {
		messages = append(messages, callbackMessage{
			emailAddress: recipient.Email,
			message:      req.MessageTxt,
			headers:      req.Headers,
		})
	}
	return c.send(ctx, messages)
}

func (c *Client) SendBulkEmail(ctx context.Context, req clients.BulkEmailRequest) (clients.HTTPResult, error) {
	messages := make([]callbackMessage, 0, len(req.Recipients))
	for _, recipient := range req.Recipients {
		if err := ctx.Err(); err != nil {
			return clients.HTTPResult{}, err
		}
		message, err := c.render(req.TemplateID, req.MessageTxt, recipient.Variables)
		if err != nil {
			return clients.HTTPResult{}, fmt.Errorf("render email for %q: %w", recipient.Email, err)
		}
		messages = append(messages, callbackMessage{
			emailAddress: recipient.Email,
			message:      message,
			headers:      req.Headers,
		})
	}
	return c.send(ctx, messages)
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

func (c *Client) send(ctx context.Context, messages []callbackMessage) (clients.HTTPResult, error) {
	if c.callback == nil {
		return clients.HTTPResult{}, ErrNilCallback
	}

	for _, message := range messages {
		if err := ctx.Err(); err != nil {
			return clients.HTTPResult{}, err
		}
		if err := c.callback(message.emailAddress, message.message, cloneHeaders(message.headers)); err != nil {
			return clients.HTTPResult{}, fmt.Errorf("send email to %q: %w", message.emailAddress, err)
		}
	}

	return clients.HTTPResult{Status: http.StatusOK}, nil
}

func cloneHeaders(headers map[string]string) map[string]string {
	if headers == nil {
		return nil
	}

	clone := make(map[string]string, len(headers))
	maps.Copy(clone, headers)
	return clone
}
