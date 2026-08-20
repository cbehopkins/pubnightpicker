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

Useful bulk flags are `--template-id`, `--template-name`, and
`--template-vars '{"name":"Alice"}'`. The request includes only the fields
whose values are supplied. `--discard-response` performs a lost-response
simulation: the HTTP request is made and printed, but recovery is given neither
`transaction_id` nor response `swg_uid` values. The response is retained only
for the final comparison report.

Set `SWEEGO_TOKEN` and `SWEEGO_PROVIDER`; `SWEEGO_BASE_URL` is optional and
defaults to `https://api.sweego.io`.

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

### Bulk facts still to observe

No live bulk request response or bulk log record is committed as a fact here.
Run `bulk-send` against Sweego and record the printed raw response and log
records in this section. In particular, verify whether bulk submission creates
one log record per recipient, where `transaction_id` appears, and whether the
returned per-recipient identifiers are keyed by address or represented as
objects. The parser's accepted shapes are implementation probes, not claims
about the provider API.

## Limitations

The prototype correlates with independent sender, recipient, time, and any
available provider/application identifiers, but it cannot prove that a missing
record means the POST was rejected. Duplicate records that satisfy the same
criteria are reported as ambiguous and retained in memory for diagnostic
output. Template names cannot be compared against logs unless Sweego exposes a
corresponding stable log field; template IDs may be supplied but are not
silently treated as campaign IDs.
