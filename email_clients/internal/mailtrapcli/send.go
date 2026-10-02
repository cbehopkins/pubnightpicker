package mailtrapcli

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"

	"email_clients/clients"
	"email_clients/clients/mailtrap"
)

type submissionOptions struct {
	from      string
	category  string
	messageID string
}

func registerSubmissionFlags(fs *flag.FlagSet, options *submissionOptions) {
	fs.StringVar(&options.from, "from", "", "sender address, optionally with display name; overrides document sender")
	fs.StringVar(&options.category, "category", "", "Mailtrap analytics category")
	fs.StringVar(&options.messageID, "message-id", "", "application correlation ID; generated when omitted")
}

func (a *app) runSend(args []string) error {
	fs := flag.NewFlagSet("send", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	var options submissionOptions
	registerSubmissionFlags(fs, &options)
	var to, subject, text, templateID, variablesJSON string
	fs.StringVar(&to, "to", "", "comma-separated recipient addresses, optionally with display names")
	fs.StringVar(&subject, "subject", "", "raw subject, supporting {{name}} placeholders")
	fs.StringVar(&text, "text", "", "raw plain-text body, supporting {{name}} placeholders")
	fs.StringVar(&templateID, "template-id", "", "Mailtrap hosted template UUID; subject and text must be empty")
	fs.StringVar(&variablesJSON, "variables", "", "JSON object of common rendering or hosted-template variables")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("send does not accept positional arguments")
	}
	from, err := parseAddress(options.from)
	if err != nil {
		return fmt.Errorf("invalid --from: %w", err)
	}
	addresses, err := mail.ParseAddressList(to)
	if err != nil || len(addresses) == 0 {
		return errors.New("--to must contain at least one valid email address")
	}
	email := clients.Email{From: from, Subject: subject, Text: text, TemplateID: templateID}
	for _, address := range addresses {
		email.To = append(email.To, clients.Recipient{Address: clients.Address{Email: address.Address, Name: address.Name}})
	}
	if variablesJSON != "" {
		if err := json.Unmarshal([]byte(variablesJSON), &email.Variables); err != nil {
			return fmt.Errorf("invalid --variables JSON object: %w", err)
		}
	}
	return a.submit(email, options)
}

type batchDocument struct {
	From      string         `json:"from"`
	Subject   string         `json:"subject"`
	Body      string         `json:"body"`
	Template  string         `json:"template"`
	Variables map[string]any `json:"variables"`
	Targets   []batchTarget  `json:"targets"`
}

type batchTarget struct {
	Dest string         `json:"dest"`
	Vars map[string]any `json:"vars"`
}

func (a *app) runBatchDocument(args []string) error {
	fs := flag.NewFlagSet("batch-send-json", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	var options submissionOptions
	registerSubmissionFlags(fs, &options)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("exactly one targets-file argument is required; place flags before the file")
	}
	email, err := loadBatchDocument(fs.Arg(0), options.from)
	if err != nil {
		return err
	}
	if len(email.To) == 0 {
		fmt.Fprintln(a.out, "No targets in document: nothing to send.")
		return nil
	}
	return a.submit(email, options)
}

func loadBatchDocument(path, fromOverride string) (clients.Email, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return clients.Email{}, fmt.Errorf("read targets file: %w", err)
	}
	var document batchDocument
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return clients.Email{}, fmt.Errorf("decode targets JSON: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return clients.Email{}, errors.New("targets file must contain exactly one JSON document")
	}
	hasBody := strings.TrimSpace(document.Body) != ""
	hasTemplate := strings.TrimSpace(document.Template) != ""
	if hasBody == hasTemplate {
		return clients.Email{}, errors.New(`supply exactly one of "body" (text-file path) or "template" (Mailtrap UUID)`)
	}
	if hasBody && strings.TrimSpace(document.Subject) == "" {
		return clients.Email{}, errors.New(`"subject" is required for raw sends`)
	}
	if hasTemplate && document.Subject != "" {
		return clients.Email{}, errors.New(`"subject" must be empty for hosted-template sends`)
	}
	fromRaw := document.From
	if fromOverride != "" {
		fromRaw = fromOverride
	}
	from, err := parseAddress(fromRaw)
	if err != nil {
		return clients.Email{}, fmt.Errorf("invalid sender: %w", err)
	}
	email := clients.Email{From: from, Subject: document.Subject, TemplateID: document.Template, Variables: document.Variables}
	if hasBody {
		bodyPath := document.Body
		if !filepath.IsAbs(bodyPath) {
			bodyPath = filepath.Join(filepath.Dir(path), bodyPath)
		}
		body, err := os.ReadFile(bodyPath)
		if err != nil {
			return clients.Email{}, fmt.Errorf("read body file: %w", err)
		}
		email.Text = string(body)
	}
	for index, target := range document.Targets {
		address, err := parseAddress(target.Dest)
		if err != nil {
			return clients.Email{}, fmt.Errorf("invalid target %d: %w", index, err)
		}
		email.To = append(email.To, clients.Recipient{Address: address, Variables: target.Vars})
	}
	return email, nil
}

func parseAddress(value string) (clients.Address, error) {
	address, err := mail.ParseAddress(strings.TrimSpace(value))
	if err != nil {
		return clients.Address{}, err
	}
	return clients.Address{Email: address.Address, Name: address.Name}, nil
}

func (a *app) submit(email clients.Email, options submissionOptions) error {
	client, cfg, err := a.client()
	if err != nil {
		return err
	}
	correlationID := strings.TrimSpace(options.messageID)
	if correlationID == "" {
		correlationID = "mt-" + rand.Text()
	}
	email.Headers = map[string]string{clients.CorrelationHeader: correlationID}
	fmt.Fprintf(a.out, "Correlation ID: %s\nSent at: %s\n", correlationID, time.Now().UTC().Format(time.RFC3339Nano))
	if cfg.Sandbox {
		fmt.Fprintln(a.out, "Sandbox mode: messages are captured, not delivered.")
	}
	result, sendErr := client.WithSendOptions(mailtrap.SendOptions{Category: options.category}).Send(context.Background(), email)
	for index, recipient := range email.To {
		pmuid := "no confirmed PMUID"
		if index < len(result.Recipients) && result.Recipients[index].PMUID != "" {
			pmuid = result.Recipients[index].PMUID
		}
		fmt.Fprintf(a.out, "Recipient %d: %s\n  PMUID: %s\n", index, recipient.Email, pmuid)
	}
	return sendErr
}
