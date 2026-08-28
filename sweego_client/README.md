# Sweego prototype client

This is a small Go CLI for inspecting Sweego email sending and log behaviour.
It is an experiment, not a production sending library. Raw provider responses
and relevant log responses are intentionally printed.

## Commands

Existing single-message experiments remain available:

```text
go run . send --from "Sender <sender@example.com>" --to recipient@example.com --subject Test --text "Hello"
go run . logs --to recipient@example.com --date 2026-08-17
go run . verify --to recipient@example.com --message-id pn-example
```

The independent bulk experiment is:

```text
go run . bulk-send \
  --from "Sender <sender@example.com>" \
  --to alice@example.com,bob@example.com,carol@example.com \
  --subject "Bulk experiment" \
  --text "Hello from the bulk experiment" \
  --attempts 10 --retry-delay 30s --recovery-window 5m
```

Useful bulk flags are `--template-id` and `--dry-run`. The request includes only
the fields whose values are supplied. `--discard-response` performs a
lost-response simulation: the HTTP request is made and printed, but recovery is
given neither `transaction_id` nor response `swg_uid` values. The response is
retained only for the final comparison report.

Set `SWEEGO_TOKEN` and `SWEEGO_PROVIDER`; `SWEEGO_BASE_URL` is optional and
defaults to `https://api.sweego.io`. The template commands additionally need
`SWEEGO_CLIENT_UUID`, the Sweego-assigned client identifier that appears in the
template endpoint path.

## Template experiments

Upload a template from a plain-text file and record the printed UUID:

```text
go run . template-upload template.txt
go run . template-upload template.txt --name pubnight-invite
```

Replace the content of an existing template:

```text
go run . template-update <template-uuid> template.txt
```

Delete one:

```text
go run . template-delete <template-uuid>
```

Deletion matters: Sweego caps stored templates per plan (observed: **5**, with
`429 {"detail":"You can create up to 5 templates on your current plan..."}`
beyond it), so experiments must tidy up after themselves.

The file is read with `os.ReadFile` and submitted verbatim in the `template`
field. Nothing is converted to HTML, escaped, trimmed, or cached locally.

Send to a list of targets described by a JSON document:

```text
go run . bulk-send-json bulk_text_request.json --dry-run
go run . bulk-send-json bulk_text_request.json --attempts 10 --retry-delay 30s
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
go run . template-upload template_document.json --name "Pubnight HTML template"
go run . bulk-send-json bulk_html_request.json --dry-run
```

`template_document.json` is such a document, with the markup at
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

The command submits `POST /send/bulk/email`, printing the status, headers, and
complete raw response body. The parser accepts the currently hypothesised
`transaction_id` plus `swg_uids` map shape and also scans nested objects for
`recipient`/`email` plus `swg_uid` pairs. This flexibility is deliberate: the
actual bulk response must be inspected before treating any shape as a provider
contract.

The output retains the relationship:

```text
Bulk operation
  transaction_id: <one provider identity>

Recipients:
  alice@example.com
    swg_uid: <individual provider identity>
```

`transaction_id` identifies the bulk submission. `swg_uid` identifies an
individual recipient message and is the identity needed for individual log,
webhook, and delivery correlation. The command never substitutes one for the
other.

Recovery queries the existing `/logs/` endpoint once per unresolved recipient,
using a day-level date range and the recipient as the search word. It then
matches locally using:

- sender;
- recipient;
- email channel;
- `email_creation` within `--recovery-window` of submission;
- transaction ID when it is available;
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

1. `go run . template-upload template.txt` and note the printed template UUID.
2. Put that UUID in a document's `template` key with a single `dest` you control,
   and run `go run . bulk-send-json <file> --dry-run`. Expect a 500, because a
   plain-text template cannot be rendered.
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
