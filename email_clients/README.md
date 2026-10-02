# Email clients prototype

This is a small Go CLI prototype for inspecting Sweego and Mailtrap email
provider integrations. It is an experiment, not a
production sending library. Relevant raw log and template responses are
intentionally printed; email submission exposes provider message IDs through
the provider-neutral client API.

## Package layout

- `cmd/sweego-client` is the executable entry point.
- `internal/cli` owns command parsing, configuration, and terminal output.
- `clients` defines the provider-neutral email, result, client, and verifier
  contracts.
- `clients/dummy` is a callback-backed test client that does not send email.
- `clients/sweego` is the importable Sweego API client for email sending and
  template administration. Additional providers will live alongside it under
  `clients/`.
- `clients/sweego/logs` owns the raw logs API, verification, matching, and
  bulk log recovery.
- `clients/mailtrap` wraps the official Go SDK for batch-first sending and
  production log verification.
- `cmd/mailtrap-client` and `internal/mailtrapcli` provide a separate Mailtrap
  inspection tool without changing the Sweego CLI.
- `internal/texttemplate` contains the simple placeholder renderer shared by
  dummy and Mailtrap raw-text sends.
- `examples` contains runnable request documents and template sources.

## Mailtrap client

Mailtrap uses the official `github.com/mailtrap/mailtrap-go` SDK, pinned to
`v0.3.0`. Configure the SDK with an API token and a bounded HTTP client, then
wrap it:

```go
sdkClient, err := sdk.NewClient(token,
  sdk.WithHTTPClient(&http.Client{Timeout: 15 * time.Second}),
)
if err != nil {
  return err
}
client, err := mailtrap.NewClient(sdkClient)
if err != nil {
  return err
}
client = client.WithSendOptions(mailtrap.SendOptions{Category: "Pub notification"})
```

Here `sdk` is the import alias for `github.com/mailtrap/mailtrap-go`; `mailtrap`
is `email_clients/clients/mailtrap`. The SDK's own options configure sandbox,
HTTP transport, host overrides and stream choice. No Sweego provider or client
UUID is needed. The wrapper has no automatic retries or durable state.

One recipient uses `/api/send`; 2-500 recipients use **one** `/api/batch`
request, with one independently addressed message per recipient. Other
recipients are not exposed in `To`. Batching uses the transactional stream by
default; Mailtrap's separate Bulk Stream is not required for batching and can
be selected programmatically with `sdk.WithBulk(true)`. Above 500 recipients,
`Send` fails before contacting the provider. Any partitioning belongs to the
caller, as separate submissions with separate correlation IDs.

### Content and partial results

Raw sends require `Subject` and `Text`. Both are rendered for every recipient
using `{{name}}` or `{{ name }}`, common variables plus recipient overrides.
All messages are rendered before the first provider request. Values use
`fmt.Sprint`, with no HTML escaping; missing variables and unsupported syntax
fail locally. This preserves the dummy client's renderer, not full Handlebars
semantics. Literal surrounding braces retain the renderer's existing behaviour.

Hosted sends use `TemplateID` as a Mailtrap template UUID and pass merged
variables to Mailtrap. `Subject` and `Text` must both be empty because the
stored template owns that content. Template administration and inline HTML are
not implemented. Venue wording, unsubscribe-link construction, sender policy
and application rate limits remain the application's responsibility.

A Mailtrap batch can partially succeed even with HTTP 200. Always consume the
result before handling the error:

```go
result, sendErr := client.Send(ctx, email)
for index, recipient := range result.Recipients {
  if recipient.PMUID != "" {
    // Persist the ID for email.To[index], even when sendErr != nil.
    recordProviderID(index, recipient.PMUID)
  }
}
if sendErr != nil {
  var batchErr *mailtrap.BatchError
  if errors.As(sendErr, &batchErr) {
    // Refusals are explicit; InvalidResults need verification, not blind retry.
    handleBatchFailure(batchErr)
  }
  return sendErr
}
```

The recording/failure functions above belong to the caller, not this package.
Result indices always refer to input recipients, including duplicate addresses.
`BatchError` separates explicit `Refusals` from `InvalidResults` and batch-level
`Messages`. An empty ID is not itself evidence of refusal. SDK API errors retain
their typed status, rate-limit advice and raw error body through `errors.As`;
successful response bodies are decoded by the SDK, not retained as raw bytes.

### Mailtrap commands

Set `MAILTRAP_TOKEN`. Optional `MAILTRAP_USE_SANDBOX=true` additionally requires
a positive `MAILTRAP_SANDBOX_ID`. Sandbox captures messages rather than
delivering them; it is not Sweego's dry-run mode. The CLI defaults to production
transactional sending, with a 15-second HTTP timeout. Help needs no token.

```text
go run ./cmd/mailtrap-client send --from "Sender <sender@example.com>" --to alice@example.com,bob@example.com --subject Test --text "Hello" --category "Test email"
go run ./cmd/mailtrap-client batch-send-json --category "Pub notification" examples/mailtrap_bulk_request.json
```

Batch documents use `from`, `subject`, `variables` (common values), and `targets`
with each `dest` and `vars`. Exactly one of `body` (a text-file path relative to
the document) or `template` (Mailtrap UUID) supplies content. Raw sends require
a subject; hosted sends require an empty/omitted subject. Unknown fields and
trailing JSON are rejected. `--from` overrides the document's sender. Flags
must precede the file argument; empty targets are a no-op with no provider call.
`--category` and `--message-id` apply to both send commands. The CLI prints
accepted IDs even on failure and exits nonzero if any send error occurs.

### Mailtrap verification

`mailtrap.NewVerifier(client, 5*time.Minute)` implements `clients.EmailVerifier`.
The adapter preserves the application correlation header and also mirrors it
into Mailtrap's `custom_variables.correlation_id`, not template variables.
Verification queries production logs by recipient and the time window, follows
all cursor pages, and matches recipient plus correlation locally. Multiple
different IDs for one pairing return `ErrAmbiguousVerification`.

Both send commands print a correlation ID and submission timestamp before
calling the provider. Record those values for a later query:

```text
go run ./cmd/mailtrap-client verify --to alice@example.com --message-id <printed-id> --sent-at <printed-RFC3339-timestamp> --verify-tolerance 5m
```

Verification returns success only when a matching log/ID is found. It does not
prove delivery. Missing logs are not proof of refusal or an instruction to
resend; ingestion lag, retention and token domain access affect visibility.
Query failures remain errors. Sandbox verification is explicitly unavailable
because production email logs are a different API from sandbox messages.

Tests use local HTTP servers and never send real Mailtrap email. Before
deployment, separately check a two-recipient sandbox batch for personalisation,
then authorise a controlled production batch to confirm correlation metadata,
message IDs, token log access and observed log visibility delay. Do not infer
production recovery behaviour from sandbox captures.

## Dummy client

The dummy client implements the shared `clients.EmailClient` interface. Create
it with a callback that receives each recipient address, rendered message, and
application headers:

```go
client := dummy.NewClient(func(emailAddress, message string, headers map[string]string) (dummy.Response, error) {
  log.Printf("email=%s message=%q headers=%v", emailAddress, message, headers)
  return dummy.Response{}, nil
})
```

`Send` invokes the callback synchronously once per recipient, in request order.
Each callback receives its own copy of `Email.Headers`.

The callback models a provider call and can answer in three ways:

- Accepted: a `dummy.Response` with an unset or 2xx `Status`. The zero value is
  an acceptance.
- Refused: a `dummy.Response` with any other `Status`, modelling a service that
  answered and declined the request. `Send` stops at that recipient and returns
  an empty `SendResult` with a `*dummy.RefusedError` carrying the recipient,
  status, and body. Recover it with `errors.As`.
- Faulted: a non-nil `error`, modelling no answer at all. `Send` stops and
  wraps it. A timeout is modelled by cancelling the context passed to `Send`.

An accepted response may supply its own `PMUID` to mimic a provider-assigned
ID; when it is empty the client generates one. Every accepted recipient gets a
distinct PMUID.

Register named bulk templates with `AddTemplate` before sending:

```go
err := client.AddTemplate("greeting", "Hello {{ name }}")
```

Dummy rendering uses Sweego's observed placeholder syntax: `{{name}}` or
`{{ name }}`. Set `Email.TemplateID` to the registered name; the client renders
the template separately for each recipient using common `Email.Variables`
combined with `Recipient.Variables`, with recipient values taking precedence.
A missing template, missing variable, unsupported placeholder syntax, or
duplicate name returns an error. All recipients are rendered before the first
callback is invoked.

Plain-text messages without a configured template use the same substitution,
matching Sweego's `message-txt` behaviour.

### Accepted hooks

After each recipient is accepted and assigned a PMUID, `Send` runs every hook
registered with `OnAccepted`, in registration order. Each hook receives a
`dummy.Accepted` carrying the correlation ID, recipient, PMUID, rendered
message, and its own copy of the headers. Refused or faulted recipients, and
those after them, trigger no hooks.

Hooks run synchronously. `dummy.Delayed(d, hook)` wraps a hook so it runs on a
new goroutine after `d`, which models provider-side work that lands later,
such as log ingestion or a delivery webhook:

```go
client.RecordAcceptedAfter(75 * time.Second) // logs appear after 50-100 s
client.OnAccepted(dummy.Delayed(2*time.Minute, func(a dummy.Accepted) {
  // emulate the delivery webhook for a.PMUID
}))
```

Delayed hooks do not see the `Send` context and cannot be cancelled. In tests,
run them inside `testing/synctest` so the delays elapse on a fake clock.

### Verification

`Client` also implements `clients.EmailVerifier`. Records are written only by
`RecordAccepted`, which must be registered as a hook, either directly for
immediate records (`client.OnAccepted(client.RecordAccepted)`) or with a delay
through `RecordAcceptedAfter`. **Without it, `Verify` never finds anything.**
Each record is keyed by the correlation ID carried in
`Email.Headers[clients.CorrelationHeader]`. `Verify` looks up a record by exact
correlation ID and case-insensitive recipient address; a recipient with no
correlation header, an unmatched correlation ID, a refusal, or a fault is never
found. This version does not persist records beyond the process and has no
search-window concept, since it holds exact records rather than provider logs.

## Commands

Existing single-message experiments remain available:

```text
go run ./cmd/sweego-client send --from "Sender <sender@example.com>" --to recipient@example.com --subject Test --text "Hello"
go run ./cmd/sweego-client logs --to recipient@example.com --date 2026-08-17
go run ./cmd/sweego-client verify --to recipient@example.com --message-id pn-example
```

The independent bulk experiment is:

```text
go run ./cmd/sweego-client bulk-send \
  --from "Sender <sender@example.com>" \
  --to alice@example.com,bob@example.com,carol@example.com \
  --subject "Bulk experiment" \
  --text "Hello from the bulk experiment" \
  --attempts 10 --retry-delay 30s --recovery-window 5m
```

Useful bulk flags are `--template-id` and `--dry-run`. The request includes only
the fields whose values are supplied. `--discard-response` performs a
lost-response simulation: the request is made, but recovery is not given the
PMUID values returned by the provider. They are retained only for the final
comparison report.

The shared `Email` API does not expose provider, campaign, or dry-run fields.
The Sweego CLI applies those as provider-specific client options. A plain
single-recipient email uses `/send`; templates, variables, or multiple
recipients use `/send/bulk/email`. Each `Send` makes at most one provider
request.

Set `SWEEGO_TOKEN` and `SWEEGO_PROVIDER`; `SWEEGO_BASE_URL` is optional and
defaults to `https://api.sweego.io`. The template commands additionally need
`SWEEGO_CLIENT_UUID`, the Sweego-assigned client identifier that appears in the
template endpoint path.

## Template experiments

Upload a template from a plain-text file and record the printed UUID:

```text
go run ./cmd/sweego-client template-upload examples/template.txt
go run ./cmd/sweego-client template-upload examples/template.txt --name pubnight-invite
```

Replace the content of an existing template:

```text
go run ./cmd/sweego-client template-update <template-uuid> examples/template.txt
```

Delete one:

```text
go run ./cmd/sweego-client template-delete <template-uuid>
```

Deletion matters: Sweego caps stored templates per plan (observed: **5**, with
`429 {"detail":"You can create up to 5 templates on your current plan..."}`
beyond it), so experiments must tidy up after themselves.

The file is read with `os.ReadFile` and submitted verbatim in the `template`
field. Nothing is converted to HTML, escaped, trimmed, or cached locally.

Send to a list of targets described by a JSON document:

```text
go run ./cmd/sweego-client bulk-send-json examples/bulk_text_request.json --dry-run
go run ./cmd/sweego-client bulk-send-json examples/bulk_text_request.json --attempts 10 --retry-delay 30s
```

The document supplies the content, the subject, the sender and the targets:

```json
{
  "body": "template.txt",
  "subject": "Pub night for {{name}}",
  "from": "Me <sender@example.com>",
  "campaign-type": "transac",
  "targets": [
    { "dest": "alice@example.com", "vars": { "name": "Alice", "date": "Friday" } }
  ]
}
```

Exactly one content source is required:

- **`body`** - a path to a plain-text file, resolved relative to the JSON
  document. Its contents are sent verbatim as `message-txt`. This is the route
  that produces a genuine `text/plain` email.
- **`template`** - a Sweego template UUID, sent as `template-id`. The template
  must be a visual-editor document; see below.

### HTML templating, working end to end

Sweego's `template` field is not free-form content of any kind. Raw HTML fails
exactly as plain text does (both `500`). What it stores is a serialised
visual-editor document, so a working template is a JSON file of that shape:

```text
go run ./cmd/sweego-client template-upload examples/template_document.json --name "Pubnight HTML template"
go run ./cmd/sweego-client bulk-send-json examples/bulk_html_request.json --dry-run
```

`examples/template_document.json` is such a document, with the markup at
`document.body.children[0].children[0].attrs.text`. Edit that string to change
the email. The client still uploads the file verbatim - the document format is
Sweego's requirement, not a transformation this client performs.

A live send using it delivered HTML to two recipients with different
`targets[].vars`, so per-recipient substitution works on the template path too.
The placeholder form in an editor document is `{{ name }}`.

The two content routes stay separate: `body` produces `text/plain` via
`message-txt`, `template` produces HTML via a stored editor document.

`subject` is required, because Sweego rejects a bulk send without one. Each
`targets[].dest` becomes a recipient `email` and each `targets[].vars` becomes
that recipient's `variables`, which are substituted into both the body and the
subject. `from` may be overridden with `--from`; `campaign-type` defaults to
`transac`. Unknown keys are rejected so typos fail loudly.

An empty `targets` array is a legitimate no-op - the command reports that there
is nothing to send and exits successfully without contacting Sweego, because the
eventual caller populates targets from a database query that may return no rows.

`bulk-send-template` remains accepted as an alias for `bulk-send-json`.

## Bulk experiment behaviour

The command submits one `POST /send/bulk/email` request. The Sweego client
parses the provider response and maps each `swg_uid` to a provider-neutral
PMUID in request order.

The output retains the relationship:

```text
Bulk operation
Recipients:
  alice@example.com
    swg_uid: <individual provider identity>
```

`swg_uid` identifies an individual recipient message and is exposed through
the generic API only as `RecipientResult.PMUID`. Transaction IDs and raw HTTP
metadata are intentionally not part of `SendResult`.

Recovery queries the existing `/logs/` endpoint once per unresolved recipient,
using a day-level date range and the recipient as the search word. It then
matches locally using:

- sender;
- recipient;
- email channel;
- `email_creation` within `--recovery-window` of submission;
- the application-owned `X-Pubnight-Message-ID` header when present.

The date range is deliberately broader than the local timestamp tolerance
because the provider log API filters dates, not times. Each attempt prints the
raw relevant log response. Recovery retries until all recipients resolve or
`--attempts` is exhausted. A recipient is reported as `RECOVERED`, `AMBIGUOUS`,
or `UNRESOLVED`; an unresolved result means only that no matching log was found
within the configured recovery window.

## Evidence ledger

### Documented by Sweego

The logs API supports date-range querying and accepts values such as `from`,
`to`, and `swg_uid`. This prototype uses the repository's established
`/logs/` request shape and `Api-Key` authentication.

Sweego's published request samples establish the template and bulk-send shapes
used here:

- `POST /clients/{uuid_client}/channels/{channel_type}/templates` creates a
  template from `{"name", "template"}`. `channel_type` is hard-coded to `email`.
- `POST /clients/{uuid_client}/channels/{channel_type}/templates/{uuid_template}`
  updates one, and additionally carries `template_type`. Update is POST, not PUT
  or PATCH, and the template UUID travels in the path.
- The template body field is called `template`. There is no separate HTML field
  and no text field, so a plain-text file is submitted as-is.
- `uuid_sms_sender_short_name` and `client_sms_sender_short_name_id` are SMS-only
  and are omitted.
- `POST /send/bulk/email` references a template through **`template-id`**, uses
  **`dry-run`**, and carries per-recipient substitution data in
  **`recipients[].variables`**.

The last point corrected three field names this client previously had wrong.
It had been sending `template_id`, `dry_run`, and a top-level `template_vars`,
none of which appear in Sweego's request body; they were being silently ignored.
The `--template-name` and `--template-vars` flags have been removed because
neither corresponds to a real request field, and the `bulk-send` two-recipient
minimum has been relaxed to one because it had no basis in the API.

### Observed experimentally in this repository

- `/logs/` date filters are day-granularity (`YYYY-MM-DD`), not time-of-day.
- Email log responses contain fields including `email_from`, `email_to`,
  `email_creation`, `status`, `swg_uid`, `transaction_id`, `campaign_id`,
  `subject`, and a `headers` map.
- Custom request headers appear lowercased in log records.
- A real single-message log was observed only after roughly 60-100 seconds;
  a five-minute recovery window is therefore a reasonable starting experiment.
- The single-message `/send` response has `channel`, `provider`, `swg_uids`,
  and `transaction_id`.

### Observed while probing for the client UUID

Read-only `GET` probes against `api.sweego.io` with a sending API key:

- `Api-Key: <token>` **does** authenticate the client-scoped template routes.
  `GET /clients/<uuid>/channels/email/templates` returns
  `404 {"detail":"Cannot found given resource: <uuid>, type: client"}` - a
  resource error, not an auth error. `Authorization: Bearer <api-key>` instead
  returns `401 {"detail":"Malformed token."}`, so the Bearer form in Sweego's
  samples expects an OAuth token, not an API key.
- `GET /clients/<uuid>/channels/email/templates` is therefore a real route, which
  means stored templates can be read back once the client UUID is known.
- `GET /clients` returns `405 Method Not Allowed`, and `GET /clients/me` returns
  `422` complaining that the path segment `uuid_client` is not a valid UUID. The
  client UUID cannot be discovered from the API with a sending key; it must come
  from the Sweego dashboard.
- The API key is itself a UUID but is **not** the client UUID; using it as one
  returns `404 ... type: client`.
- `GET /channels` returns `[{"id":1,"name":"email"},{"id":2,"name":"sms"}]`.
- `/me`, `/account`, `/accounts`, `/users/me`, `/api-keys`, `/senders`,
  `/domains`, `/campaigns`, `/templates`, `/webhooks` are all 404.
- The `/logs/` response's `senders` field is a domain string (`swg-srv.net`), not
  an object carrying client identifiers.

### Bulk facts still to observe

No live bulk request response or bulk log record is committed as a fact here.
Run `bulk-send` against Sweego and record the printed raw response and log
records in this section. In particular, verify whether bulk submission creates
one log record per recipient, where `transaction_id` appears, and whether the
returned per-recipient identifiers are keyed by address or represented as
objects. The parser's accepted shapes are implementation probes, not claims
about the provider API.

### Template facts established live (2026-08-28)

- `POST /clients/{client}/channels/email/templates` returns **201** and echoes the
  stored record `{name, template, uuid}`. The created UUID is in `uuid`.
- The plain-text file was stored **byte-for-byte**. Reading it back with
  `GET .../templates/{uuid}` returns exactly the bytes that were sent. Sweego
  does not convert the content on upload.
- **A plain-text template cannot be used to send.** `POST /send/bulk/email` with
  `template-id` pointing at it returns `500 {"detail":"Internal server error"}`.
- The cause is the template content format, not the `template-id` mechanism.
  Two sends differing only in `template-id` gave: UI-built template `200`,
  plain-text template `500`.
- A template created in the Sweego UI stores, in the same `template` field, a
  serialised **visual-editor JSON document**:
  `{"document":{"title":...,"body":{"children":[...]}},"trigger":"auto-save"}`,
  with the actual markup nested at `attrs.text` as `<p>Hello {{ name }}</p>`.
  So `template` is not free-form content; Sweego parses it as that document
  structure at send time, and arbitrary text makes the renderer fail.
- `subject` is required even when `template-id` is supplied (omitting it gives
  `422 field required`).
- A bulk send with `message-txt` and no template works normally (`200`), so the
  existing non-template path is unaffected.
- The UI template's placeholder syntax is `{{ name }}` (with spaces).
- **Raw HTML in `template` fails identically to plain text (`500`).** The
  constraint is the document format, not the content type.
- A hand-built editor document uploaded via `template-upload` sends successfully
  (`200`) and delivers HTML, with per-recipient `variables` substituted. So
  template-based sending does work - it just requires that JSON document shape.
- `DELETE /clients/{client}/channels/email/templates/{template}` removes a
  template; a subsequent `GET` returns 404.
- Sweego caps stored templates per plan: the sixth create returned
  `429 {"detail":"You can create up to 5 templates on your current plan. Upgrade to create more!"}`.

**Conclusion: Sweego email templates do not support plain-text source content,
and therefore cannot produce a genuine `text/plain` email.** The
`template-upload` command stores plain text successfully, but that upload proves
only that the bytes were persisted - it does not make them sendable.

### The working plain-text route: `message-txt` with per-recipient variables

Templates are not needed for personalised plain-text mail. A bulk send carrying
`message-txt` and no `template-id` substitutes each recipient's `variables` into
the body. Verified by a real delivery: a body of

```text
Body nospace={{name}}
Body spaced={{ name }}
Date={{date}}
```

sent with `recipients[0].variables = {"name":"SUBSTITUTED","date":"Friday"}`
arrived as

```text
Body nospace=SUBSTITUTED
Body spaced=SUBSTITUTED
Date=Friday
```

So substitution works in `message-txt`, and both `{{name}}` and `{{ name }}`
spacing variants are honoured. Substitution also applies to `subject`: a subject
of `PN subj nospace={{name}} spaced={{ name }}` was recorded in `/logs/` as
`PN subj nospace=SUBSTITUTED spaced=SUBSTITUTED`.

**The delivered message's raw source showed `Content-Type: text/plain`.** So
Sweego does deliver genuine plain-text email - via `message-txt`, not via
templates. This is the route the `body` key in the JSON document uses.

### Announced Sweego API changes

- From **16 September 2026**, `channel` becomes mandatory on `/send` for both
  email and SMS; it previously defaulted to `email` when omitted. This client is
  already compliant: every request sets `Channel: "email"` explicitly, and the
  struct tag carries no `omitempty`, so the field is always serialised. Keep it
  that way - do not add `omitempty` to `channel`.

### Still unverified

- The MIME structure of a template-based send. The HTML template delivers HTML,
  but whether it is `text/html` or `multipart/alternative` has not been
  inspected in the raw source.
- Whether Sweego offers any documented plain-text or non-editor template format.
- The full schema of the editor document. `template_document.json` was derived
  from a UI-built template, so which fields are genuinely required is unknown.
- Whether a bulk send is rejected when both `template-id` and `message-txt` are
  present. The client refuses to construct that combination, so it stays
  untested.

## Manual proof-of-behaviour procedure

This was carried out on 2026-08-28; the results are recorded above. To repeat it:

1. `go run ./cmd/sweego-client template-upload examples/template.txt` and note
  the printed template UUID.
2. Put that UUID in a document's `template` key with a single `dest` you control,
  and run `go run ./cmd/sweego-client bulk-send-json <file> --dry-run`. Expect
  a 500, because a plain-text template cannot be rendered.
3. Switch the document to `"body": "template.txt"` and repeat. Expect a 200.
4. Repeat without `--dry-run` to deliver a real message.
5. Open the received message and view its raw source, not the rendered body.
6. Confirm the top-level `Content-Type` is `text/plain`, and that `{{name}}` and
   `{{date}}` were substituted in both the body and the subject.

## Live behaviour tests

`go test ./...` is hermetic: every test uses a local `httptest` server and needs
no credentials.

The findings below are claims about a third party, so `live_test.go` re-checks
them against the real API. It is skipped unless `SWEEGO_LIVE_TESTS` is set:

```text
$env:SWEEGO_LIVE_TESTS = '1'
$env:SWEEGO_LIVE_FROM  = 'Me <sender@example.com>'
$env:SWEEGO_LIVE_TO    = 'recipient@example.com'
$env:SWEEGO_LIVE_UI_TEMPLATE_UUID = '<a template built in the Sweego UI>'
go test -run TestLive -v .
```

It also needs `SWEEGO_TOKEN`, `SWEEGO_PROVIDER` and `SWEEGO_CLIENT_UUID`. Every
send uses `dry-run`, so no email is delivered.

The tests assert that a plain-text template is stored unchanged, that sending
with it returns 500, that raw HTML fails the same way, that a hand-built editor
document returns 200, that a UI-built template returns 200, that `message-txt`
returns 200, that `DELETE` removes a template, and that `subject` is mandatory.
The negative assertions are deliberate: if Sweego ever starts accepting
plain-text templates the test fails, which is the signal that the conclusions
below have gone stale.

Every template created by the tests is deleted on cleanup, because the plan caps
stored templates at five.

## Limitations

The prototype correlates with independent sender, recipient, time, and any
available provider/application identifiers, but it cannot prove that a missing
record means the POST was rejected. Duplicate records that satisfy the same
criteria are reported as ambiguous and retained in memory for diagnostic
output. Template names cannot be compared against logs unless Sweego exposes a
corresponding stable log field; template IDs may be supplied but are not
silently treated as campaign IDs.

The `template-upload` and `template-update` commands report only what Sweego
returned. They do not read the template back, so they cannot confirm that the
stored content matches what was sent.
