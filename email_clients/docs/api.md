# Email Client API

## 1. Scope

This document defines the provider-neutral API implemented by the raw email client layer.

The raw email client is responsible for communicating with an external email provider. It is deliberately independent of Cellar, application persistence, email delivery state, and durable recovery.

A separate durable email layer will wrap this API and use Cellar to make email submission recoverable.

---

## 2. Email API

### Address

```go
type Address struct {
    Email string
    Name  string
}
```

### Recipient

A recipient consists of an address and optional recipient-specific template variables.

```go
type Recipient struct {
    Address
    Variables map[string]any
}
```

### Email

`Email` describes one logical email submission.

```go
type Email struct {
    From       Address
    To         []Recipient
    Subject    string
    TemplateID string
    Text       string
    Variables  map[string]any
    Headers    map[string]string
}
```

Field semantics:

* `From` — sender address.
* `To` — one or more recipients.
* `Subject` — email subject.
* `TemplateID` — opaque provider-side template identifier.
* `Text` — complete plain-text version of the message.
* `Variables` — variables common to all recipients.
* `Headers` — custom email headers.

`Headers` applies to the whole request and is therefore identical for every
recipient. The API does not support per-recipient headers, because the
underlying provider requests carry headers only at request level.

An application correlation identifier placed in a header therefore identifies
the Send operation rather than one recipient. A single recipient is identified
by that correlation identifier combined with the recipient address, which is
the pairing the verification API expects.

For a recipient, the effective template variables are the common `Email.Variables` combined with that recipient's `Variables`, with recipient-specific values taking precedence on collision.

`TemplateID` and `Text` may both be present. A templated email can therefore use a provider-hosted template for its rich content while supplying its plain-text alternative through `Text`.

Provider capabilities can restrict this combination. Mailtrap hosted templates
own the subject and both body formats: its adapter requires empty `Subject` and
`Text` whenever `TemplateID` is set, and rejects conflicting content before
sending. It never silently drops content or retrieves templates to render them
locally. Without `TemplateID`, Mailtrap renders `Subject` and `Text` using the
dummy client's simple `{{name}}` / `{{ name }}` placeholders. This is not a
Handlebars engine; hosted Mailtrap templates use the provider's own rendering.

The API deliberately does not expose:

* template engines
* HTML
* provider template management
* attachments
* provider selection
* marketing/transactional classifications
* provider-specific request types

---

## 3. Sending

The raw client interface is:

```go
type EmailClient interface {
    Send(context.Context, Email) (SendResult, error)
}
```

### One Send = at most one provider request

A single invocation of `Send` represents one logical email operation and may result in **at most one external provider request**.

The client must not silently split one `Send` into multiple provider requests.

This is important because the durable layer relies on the external operation
being a single submission from the application's perspective. A single HTTP
request does not imply all-or-nothing acceptance: providers can accept some
messages and refuse others in the same batch.

If a provider cannot support the requested operation with a single external request, that provider implementation must return an error rather than implementing its own fan-out.

---

## 4. SendResult

The result deliberately contains only information learned from the provider's submission response.

```go
type SendResult struct {
    Recipients []RecipientResult
}

type RecipientResult struct {
    PMUID string
}
```

`Recipients` is positionally aligned with `Email.To`: the result at each index
describes the submitted recipient at the same index.

When `Send` succeeds, every input recipient has a non-empty `PMUID`. A client
can also return a partial `SendResult` together with an error. Such a result
retains all input indices: accepted recipients have their provider IDs, and
entries without a confirmed ID are empty. Callers must inspect and preserve
these results even when `err != nil`; they must not blindly retry the batch.

An empty `PMUID` alone is not proof of refusal. Mailtrap's `*mailtrap.BatchError`
exposes `Refusals` (explicit provider refusal), `InvalidResults` (unusable or
contradictory item responses), and batch-level `Messages`. Per-recipient
diagnostics carry the original `Index`, `Recipient`, and provider messages.
`errors.Is(err, mailtrap.ErrInvalidResponse)` identifies invalid item responses.
If the response count is wrong or the request/response cannot be decoded, the
adapter returns no guessed mapping. Whole-request SDK errors remain accessible
through `errors.As`, and context errors through `errors.Is`.

Existing clients may still return an empty result on failure. No client is
required to invent provider IDs or submission state when evidence is absent.

`PMUID` means **provider message UID**.

It is the provider-neutral representation of the unique identifier assigned by the external email service to an individual message.

Examples:

```text
Sweego swg_uid       -> PMUID
Dummy generated ID   -> PMUID
Mailtrap message_id  -> PMUID
```

The provider-specific identifier must not leak through the generic API.

### No submission/recovery state

`SendResult` does not contain concepts such as:

* Pending
* Recovery
* RecoveryWaiting
* Accepted
* uncertain submission
* retry required

Those are properties of the durable email layer.

If the process dies after the provider request but before the result is durably recorded, the durable layer assumes the worst case and uses its Recovery mechanism to query the provider later.

---

# 5. Verification API

Submission and recovery verification are separate capabilities.

The raw provider implementation exposes:

```go
type VerifyRequest struct {
    CorrelationID string
    Recipient     string
    SentAt        time.Time
}
```

and:

```go
type VerifyResult struct {
    Found bool
    PMUID string
}
```

with:

```go
type EmailVerifier interface {
    Verify(context.Context, VerifyRequest) (VerifyResult, error)
}
```

## Verification semantics

The durable layer asks:

> Has the provider recorded a message corresponding to this application's correlation ID and recipient, and if so what is its provider message UID?

Both values are required. A correlation ID may cover several recipients of one
Send operation, so it does not identify a message on its own.

If:

```go
result.Found == true
```

then `PMUID` must identify the provider message.

The durable layer can therefore transition:

```text
RecoveryWaiting -> Accepted
```

and record `PMUID`.

If:

```go
result.Found == false
```

then there is no evidence that the provider accepted the message.

The durable layer can therefore transition:

```text
RecoveryWaiting -> Pending
```

and allow the normal posting process to retry it.

---

## 6. Verification search window

`VerifyRequest` contains `SentAt`, but does not contain the search tolerance.

The durable email layer owns the recovery policy:

```text
Recovery -> wait 120 seconds -> verify
```

The provider verifier owns the details of how it searches its logs.

The current Sweego implementation uses a time tolerance around the send attempt. That remains an implementation detail of the Sweego verifier rather than becoming part of the generic API.

---

## 7. Correlation

The application correlation identifier is carried in:

```text
X-Pubnight-Message-ID
```

through:

```go
Email.Headers
```

The same correlation ID is retained in the durable recipient record.

Verification uses that correlation ID together with the recipient address and send timestamp.

This allows provider logs to be searched without exposing provider-specific correlation mechanisms through the generic API.

---

# 8. Responsibility Boundaries

### Raw Email Client

Responsible for:

* provider API requests
* provider request construction
* provider response parsing
* provider-specific template selection
* provider-specific error handling
* mapping provider message IDs to `PMUID`

Not responsible for:

* Cellar
* SQLite/application persistence
* durable state
* retry policy
* recovery workflow
* `ApplicationWork`
* `CellRequest`

### Email Verifier

Responsible for:

* querying provider-side logs
* locating a message using the generic verification request
* mapping the provider message identifier to `PMUID`

Not responsible for:

* changing durable application state
* scheduling recovery
* deciding whether to retry

### Durable Email Layer

Responsible for:

* recipient records
* Send idempotency token
* Pending/Recovery/RecoveryWaiting/Accepted/Sent/Rejected states
* startup recovery
* the 120-second recovery delay
* invoking `EmailClient.Send`
* invoking `EmailVerifier.Verify`
* translating results into `ApplicationWork`
* creating Cellar continuation Cells
* processing webhook state changes

### Application

Responsible only for:

* deciding that an email should be sent
* constructing the logical `Email`
* connecting a Domain Truth to the durable email operation

---

# 9. Example

A mailing-list send could look conceptually like:

```go
email := Email{
    From: Address{
        Email: "pubnight@example.com",
        Name:  "Pubnight",
    },
    To: []Recipient{
        {
            Address: Address{
                Email: "alice@example.com",
                Name:  "Alice",
            },
            Variables: map[string]any{
                "unsubscribe_url": "...",
            },
        },
        {
            Address: Address{
                Email: "bob@example.com",
                Name:  "Bob",
            },
            Variables: map[string]any{
                "unsubscribe_url": "...",
            },
        },
    },
    Subject:    "This week's pub",
    TemplateID: "weekly-pub",
    Text:       "This week's pub is the Red Lion.",
    Variables: map[string]any{
        "pub_name": "The Red Lion",
    },
    Headers: map[string]string{
        "X-Pubnight-Message-ID": "application-correlation-id",
    },
}

result, err := client.Send(ctx, email)
```

A successful result might contain:

```go
SendResult{
    Recipients: []RecipientResult{
        {PMUID: "provider-message-id-1"},
        {PMUID: "provider-message-id-2"},
    },
}
```

The application does not need to know whether those IDs are Sweego IDs, Mailtrap IDs, or Dummy IDs.

---

# 10. Design Principle

The API should remain deliberately boring.

The raw client answers:

> "Submit this logical email to the provider, and tell me the provider message IDs that the submission response gives me."

The verifier answers:

> "Can you find this previously submitted message in the provider's records, and if so, what is its provider message ID?"

Everything concerned with making those operations durable belongs above these APIs.
