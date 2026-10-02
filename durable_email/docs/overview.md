# CDD — Durable Email Delivery

## 1. Purpose

This document defines the durable email delivery component that sits between the application and the provider-neutral `email_clients` API.

The component makes email submission durable using Cellar. It records per-recipient progress, handles ambiguous provider submission outcomes, performs provider-log recovery, and records eventual delivery results reported by provider webhooks.

The component is deliberately separate from the raw email client.

The raw email client is responsible only for communicating with an external email provider.

The durable email delivery component is responsible for:

* creating durable progress records;
* creating and executing Cellar sequences;
* submitting pending email recipients;
* recovering ambiguous submissions;
* maintaining per-recipient state;
* creating verification work;
* processing provider webhook results.

---

# 2. Responsibilities

## 2.1 Raw Email Client

The raw `EmailClient`:

```go
type EmailClient interface {
    Send(context.Context, Email) (SendResult, error)
}
```

is responsible for communicating with the provider.

It:

* constructs the provider request;
* chooses the appropriate provider endpoint;
* performs at most one provider request for one `Send` call;
* maps the provider response into `SendResult`;
* returns the provider's per-recipient PMUID where available.

It does not know about:

* Cellar;
* the progress table;
* application transactions;
* durable state;
* recovery state;
* Truths.

The raw `EmailVerifier`:

```go
type EmailVerifier interface {
    Verify(context.Context, VerifyRequest) (VerifyResult, error)
}
```

is responsible for querying provider-side records to determine whether an ambiguous submission can be identified.

---

## 2.2 Pre-submission guard

Registration may supply a `SubmissionGuard` through optional `RegisterOptions`.
The guard receives the immutable idempotency token and the number of recipients
still pending for this provider attempt. It is independent of application policy.

Post invokes the guard after recovery and empty-recipient checks, immediately
before recording submission timestamps. Zero delay permits submission; a positive
delay retries the current Post step at that future time. Errors and negative
delays fail closed. Refusal does not call the provider, change recipient state or
submission timestamps, create recovery work, or run later sequence steps.

The guard runs again on each submission attempt, including attempts involving
only a subset of the original recipients after recovery. Token reservations and
refunds are not part of this component. Omitting the guard preserves existing
standalone behaviour. Deferred Cell scheduling is persisted by Cellar; no new
email schema or persisted guard state is required. Verification is not guarded.

---

# 3. Durable Request and Progress

The durable email component maintains four related tables:

* `email_requests` contains one row per logical Send operation;
* `email_progress` contains one row per recipient of that operation;
* `email_events` records provider events for each recipient;
* `email_ownership` associates a recipient send with its application user and creation time for private reporting.

Together the tables contain the provider-neutral request data and per-recipient
progress required to execute and recover the delivery operation. They are not
an independent source of truth for application email configuration.

## 3.1 Schema

The logical schema is:

```sql
CREATE TABLE email_requests (
    idempotency_token TEXT NOT NULL PRIMARY KEY,
    message_id        TEXT NOT NULL UNIQUE,
    sender_email      TEXT NOT NULL,
    sender_name       TEXT NOT NULL,
    subject           TEXT NOT NULL,
    template_id       TEXT NOT NULL,
    text              TEXT NOT NULL,
    variables         TEXT NOT NULL,
    headers           TEXT NOT NULL
);

CREATE TABLE email_progress (
    idempotency_token TEXT NOT NULL,
    recipient        TEXT NOT NULL,
    recipient_name   TEXT NOT NULL,
    state            TEXT NOT NULL,
    pmuid            TEXT,
    variables        TEXT NOT NULL,
    submitted_at     DATETIME,

    PRIMARY KEY (idempotency_token, recipient)
);

CREATE TABLE email_events (
    id                INTEGER PRIMARY KEY,
    idempotency_token TEXT NOT NULL,
    recipient         TEXT NOT NULL,
    event             TEXT NOT NULL,
    recorded_at       DATETIME NOT NULL
);

CREATE TABLE email_ownership (
    user_id           TEXT NOT NULL,
    idempotency_token TEXT NOT NULL,
    recipient         TEXT NOT NULL,
    created_at        DATETIME NOT NULL,
    PRIMARY KEY (user_id, idempotency_token, recipient),
    UNIQUE (idempotency_token, recipient)
);
```

`SendRecipient.UserID` is optional application-supplied ownership metadata, not
a provider template variable. Setup persists ownership atomically with the send;
retries cannot change the owner or creation time. Application callers must derive
it from trusted user identities. Unowned sends remain valid for delivery but are
excluded from UID-filtered reporting. Existing databases gain an empty ownership
table; ownership is never inferred from an email address or backfilled.

`ProgressFilter.UserID` filters `QueryProgress` by recorded ownership.
`LatestFirst` orders by creation time descending with recipient row ID as a stable
tie-breaker, before applying the limit. The default primary-key order remains
unchanged. `ProgressRow.CreatedAt` is absent for unowned sends.

The exact SQLite declaration may be adjusted to match the final database migration conventions, but the logical fields and constraints are normative. `email_events` is an append-only record of provider events; `recorded_at` is when the durable layer received the event, not a claim about provider event time. Repeated opens and clicks remain separate records. Existing databases gain the new table on `NewStore` without changing their progress rows.

## 3.2 Request fields

### `idempotency_token`

Identifies the logical Send transaction.

Several Send transactions may be in progress simultaneously.

The token is the identity of the request row and forms part of the identity of
each related progress row.

An idempotency token identifies one immutable logical request, including its
recipient set. Reusing a token with different request data or recipients is an
error; existing data must not be silently merged or replaced.

### `message_id`

The durable correlation identifier used as the value of:

```text
X-Pubnight-Message-ID
```

It belongs to the Send operation, not to an individual recipient.

Provider headers are request-level and identical for every recipient of one
provider request, so a per-recipient correlation header cannot be expressed.
A recipient is therefore identified by the pair:

```text
(message_id, recipient)
```

This pair is used both for provider-log verification and for webhook
correlation.

### Sender, content, variables, and headers

`sender_email`, `sender_name`, `subject`, `template_id`, and `text` preserve the
corresponding fields of the provider-neutral `Email` request. Empty strings are
stored as empty strings rather than interpreted as missing values.

`variables` stores the operation-wide template variables as a JSON object.
`headers` stores the application-supplied headers as a JSON object. Empty maps
are stored as `{}`.

The durable layer owns `X-Pubnight-Message-ID`. When reconstructing a request,
it sets that header from the request's `message_id`, overriding any value
present in the stored common headers.

## 3.3 Progress fields

### `recipient`

The recipient email address.

A separate progress row exists for each recipient.

### `recipient_name`

The recipient display name supplied in the provider-neutral `Recipient`.

It is stored per recipient because display names may differ within one logical
Send operation.

### `state`

The current durable state of the recipient.

The allowed states are defined in Section 4.

### `pmuid`

The provider message identifier.

It is populated once provider acceptance has been established, either directly from `EmailClient.Send` or through recovery verification.

It is NULL until then.

The durable layer treats this value as opaque. Its provider-specific representation is mapped to the generic `PMUID` type used by the raw API.

### `variables`

The recipient-specific template variables required to construct the provider submission.

The value is stored in a form suitable for reconstruction by the durable email component.

For a direct non-templated email this will normally be an empty variable map.

### `submitted_at`

The time of the latest provider submission attempt for this recipient.

It is NULL before the first attempt. It is recorded before calling
`EmailClient.Send`, so an interruption after the external request still leaves
a timestamp suitable for provider verification. Recipients submitted in one
provider request share a submission time, but later retries may submit a subset
at a different time, so this value belongs to the progress row rather than the
request row.

---

# 4. Progress State Machine

Each recipient has a current progress state and a separate history of provider
events. The state answers what is currently known; the event history preserves
repeated opens and clicks and reports events that must not replace a stronger
state. Neither tracking nor a delivery event makes a recipient eligible for
another Post.

## 4.1 States

### `Pending`

The recipient has not yet been established as accepted by the provider.

A Pending recipient is eligible for provider submission.

This is the initial state of every progress row.

### `Accepted`

The provider has accepted the recipient submission and a provider message identifier (`PMUID`) is known.

`Accepted` does not mean that the message has been delivered to the recipient.

An Accepted row awaits the provider's eventual delivery result.

### `Sent`

Sweego reports that it has dispatched the message. This is not proof of
delivery and is not terminal.

### `Delivered`

Sweego reports delivery. A human open, click or spam complaint may supersede
this state.

### `Opened` and `Clicked`

A human open or click respectively has been reported. Proxy opens do not imply
a human open and do not set `Opened`. A click implies the message was opened;
neither outcome can be downgraded by a late delivery event.

### `SpamComplaint`

The recipient has complained about the message. This overrides even a prior
delivery, open or click. It is terminal.

### `HardBounced`

The provider reports a permanent delivery failure. It is terminal; a soft
bounce is not and is recorded as an event without a terminal state change.

### `Refused`

The provider definitively and permanently refused the recipient at Post,
before acceptance. It is terminal. A temporary refusal requires an explicit
retry policy; an ambiguous response requires recovery, not `Refused`.

The provider-neutral `EmailClient` result does not yet classify a refusal as
permanent. Until that contract and provider-specific classification are added,
Post must not infer `Refused` from an error or a missing PMUID. The state is
reserved for a definitive permanent refusal, not for all provider errors.

### `Recovery`

The previous submission attempt produced no usable provider result, so the system cannot determine whether the provider received the request.

`Recovery` is therefore an ambiguous-submission state.

It is not permission to immediately submit the email again.

### `RecoveryWaiting`

The recipient has entered provider-log recovery and is waiting for provider records to become visible.

The recovery handler schedules a delayed Fanout; its individual Verify Cells
query the provider after that delay.

For the initial implementation the delay is **120 seconds**.

---

# 5. State Transitions

The following transitions are permitted.

| Current state     | Event                                      | New state         |
| ----------------- | ------------------------------------------ | ----------------- |
| —                 | New Send creates recipient                 | `Pending`         |
| `Pending`         | `Send` returns usable PMUID                | `Accepted`        |
| `Pending`         | Definitive permanent refusal              | `Refused`         |
| `Pending`         | `Send` returns an error / no usable result | `Recovery`        |
| `Recovery`        | Recovery handler schedules verification    | `RecoveryWaiting` |
| `RecoveryWaiting` | Verification finds provider message        | `Accepted`        |
| `RecoveryWaiting` | Verification finds no provider message     | `Pending`         |
| `Accepted`        | Sweego Sent                                | `Sent`            |
| `Accepted`, `Sent` | Sweego Delivered                           | `Delivered`       |
| `Accepted`, `Sent`, `Delivered` | Human open                | `Opened`          |
| `Accepted`, `Sent`, `Delivered`, `Opened` | Click          | `Clicked`         |
| `Accepted`, `Sent` | Hard bounce                                | `HardBounced`     |
| `Accepted`, `Sent`, `Delivered`, `Opened`, `Clicked` | Spam complaint | `SpamComplaint` |
| `Accepted`, `Sent`, `Delivered`, `Opened`, `Clicked` | Soft bounce or proxy open | unchanged |

Webhook events are recorded even when their state transition is unchanged.
Late lower-priority events cannot undo `Delivered`, `Opened`, `Clicked` or
`SpamComplaint`. Repeated events do not change an already reached state.
Delivery and tracking events may arrive before earlier webhook events; a human
open or click can therefore advance directly from `Accepted`. A correlated
webhook may also precede the Post result: it is evidence of submission and
advances a `Pending`, `Recovery`, or `RecoveryWaiting` row to its reported
state. A soft bounce or proxy open in that situation advances it to `Sent`,
so it cannot be resent while the provider has already seen it. Post and Verify
may not downgrade such a row. `Refused` and
`HardBounced` do not change in response to subsequent delivery or tracking
events. No other transitions are valid.

In particular:

* no provider event can return a recipient to `Pending` or `Accepted`;
* `Refused` and `SpamComplaint` cannot return to a sending state;
* `Recovery` does not directly perform another provider submission;
* `RecoveryWaiting` does not directly perform another provider submission.

---

# 6. Startup Recovery

Startup recovery is an application-level operation.

It is not a Cellar Recovery Cell and does not create a separate recovery workflow.

Before starting Cellar, the application invokes durable email startup recovery to
change rows that are still `Pending` with a non-NULL `submitted_at`:

```text
Pending → Recovery
```

for all appropriate progress rows.

This protects against the possibility that a process stopped after a provider submission was made but before the durable result was recorded.

Rows never submitted, and rows already accepted, are left alone. The
already-persisted Send sequences remain responsible for processing the rows.
Cellar restores a claimed Cell at its previous step; if that step is Post,
Post restarts the sequence before making another provider call.

---

# 7. Send Sequence

A logical Send creates a Cellar Sequence consisting of three steps:

```text
Setup
  ↓
Recovery
  ↓
Post
```

The sequence is durable and survives process restart.

---

# 8. Setup Handler

The Setup handler establishes the durable request and progress records required
by the Send operation.

It creates one `email_requests` row containing the operation-wide fields and a
unique `message_id` for the operation. For each recipient it then:

1. creates one `email_progress` row;
2. records the Send transaction's idempotency token;
3. records the recipient address and display name;
4. records the recipient's template variables;
5. sets `submitted_at` to NULL;
6. sets the initial state to `Pending`.

The request row and all recipient rows are created in one Cellar-managed
application transaction. They must not become visible independently.

The Setup handler does not communicate with the email provider.

---

# 9. Recovery Handler

The Recovery handler first examines the associated progress rows.

During ordinary execution there will normally be no `Recovery` rows, and the handler completes.

After an ambiguous Post or during startup recovery, rows may have been changed
to `Recovery`.

The handler changes eligible rows:

```text
Recovery → RecoveryWaiting
```

and atomically creates one Fanout Cell with `NotBefore` no earlier than the
latest affected `submitted_at` plus 120 seconds. It retries its own step no
earlier than the same deadline.

This uses Cellar's scheduling mechanism rather than blocking a worker.

The Fanout creates one keyed Verify Cell per `RecoveryWaiting` recipient. Each
Verify Cell independently asks the provider about its recipient and atomically
records one outcome with its own completion:

1. if verification finds the provider message, it records the PMUID and changes
    the row to `Accepted`;
2. if verification succeeds but finds no provider message, it changes the row
    to `Pending` and clears `submitted_at`;
3. if verification fails, the row stays `RecoveryWaiting` and Post cannot run.

Recovery retries while any `RecoveryWaiting` rows remain. Only after every
recipient is resolved does it complete and allow Post to submit those now
`Pending`. One provider check after the delay is sufficient to establish
absence; repeated checks for a successful `Found == false` are not required.

The delay is measured from the persisted timestamp recorded immediately before
the provider call. A crash before the call may therefore incur an unnecessary
verification, but cannot cause an immediate duplicate submission.

A verification error is an operational failure of that recipient's Verify Cell,
handled using Cellar's error/recovery semantics. Other recipients can finish
independently; an unresolved recipient never releases Post.

The recovery operation never sends an email itself.

---

# 10. Provider Verification

For each recovery candidate the durable layer constructs:

```go
type VerifyRequest struct {
    CorrelationID string
    Recipient     string
    SentAt        time.Time
}
```

`CorrelationID` is the request's `message_id`.

Because one correlation ID covers every recipient of the operation, the
correlation ID alone does not identify a recipient. The verifier combines it
with `Recipient` to select the provider record.

A correlation ID may cover more than one submission attempt for the same
recipient, because a retry after recovery reuses it. Any matching provider
record represents the same logical message; the PMUID identifies the specific
attempt.

The provider-specific verifier uses:

* correlation ID;
* recipient;
* submission timestamp;

to search provider records.

Provider-specific search tolerances remain implementation details of the provider verifier.

If a matching provider message is found:

```go
VerifyResult{
    Found: true,
    PMUID: "...",
}
```

is returned.

If the provider query succeeds but no message is found:

```go
VerifyResult{
    Found: false,
}
```

is returned.

`Found == false` is not an error.

---

# 11. Post Handler

The Post handler processes `Pending` progress rows.

It reconstructs one provider-neutral `Email` containing **all** pending
recipients of the operation and calls:

```go
EmailClient.Send(...)
```

Reconstruction combines the operation-wide fields from `email_requests` with
the recipient address, display name, and variables of each pending row. The
durable layer sets `X-Pubnight-Message-ID` from the request's `message_id`.
Provider configuration remains an injected runtime dependency and is not part
of the durable request data.

The handler must not issue one provider request per recipient. The Cell is the
unit of idempotency for exactly one external call, and the raw client API
requires that one `Send` produce at most one provider request.

`SendResult.Recipients` is positionally aligned with the submitted recipients,
so each recipient's PMUID is taken from the corresponding entry.

Immediately before the external call, the handler records `submitted_at` for
every recipient in the batch. This deliberately favours an unnecessary
verification delay after a crash before the call over an untraceable submission
after a crash following the call.

A logical Send operation must result in **at most one external provider request per invocation of `EmailClient.Send`**.

The durable layer must not assume that an error means that the provider definitely did not receive the request.

Therefore:

* usable provider result → `Accepted`;
* error / no usable result → `Recovery` and restart at Setup using
    `RetrySequence` with the progress update in `ApplicationWork`.

Setup must accept an identical existing request without replacing the message
ID or resetting recipient progress; a conflicting reuse of the token fails.
Recovery then delays and verifies before a subsequent Post may run. On startup
Post also checks for unresolved recovery rows before sending, since Cellar may
resume an interrupted Cell directly at Post.

The provider PMUID returned by `SendResult` is stored in the progress row when the result is usable.
If a correlated webhook has advanced the row before Post or Verify commits,
record the PMUID without downgrading the webhook state.

The correlation/message ID stored in the request row is supplied to the provider through:

```text
X-Pubnight-Message-ID
```

---

# 12. Atomic Cellar Completion

The final Post processing is not simply a final database update followed by a separately-created Cell.

When Post completes, any follow-up Query Cells required for the individual recipients are created using Cellar's `Complete.NewCells`.

The progress-table updates and creation of those Cells therefore occur as one Cellar-managed atomic operation.

This ensures that durable application progress cannot be committed without the corresponding follow-up work also being committed.

---

# 13. Webhook Processing

The Sweego webhook adapter authenticates and parses provider payloads, then
calls the durable layer's provider-neutral method. The durable layer does not
implement an HTTP endpoint or parse Sweego payloads. Mailtrap webhooks are out
of scope.

The provider webhook supplies the email headers, including:

```text
X-Pubnight-Message-ID
```

The message ID identifies the Send operation. Combined with the recipient
address reported by the event, it identifies the corresponding progress row.

The webhook adapter calls:

```go
func (s *Store) RecordEvent(ctx context.Context, messageID, recipient string, event DeliveryEvent) error
```

`DeliveryEvent` is one of `Sent`, `Delivered`, `SoftBounce`, `HardBounce`,
`ProxyOpen`, `HumanOpen`, `Click`, or `SpamComplaint`. List-unsubscribe is
deliberately ignored. Unknown event types and unknown `(messageID, recipient)`
pairs return errors. The method atomically appends a provider event and updates
the recipient's state using Section 5. Tracking events are retained even if
they do not change the current state. Once `SpamComplaint` has been recorded,
subsequent provider events cannot conceal it. Duplicate webhook deliveries
may produce duplicate event records until the provider's event identity and
deduplication rules are established; state changes remain idempotent.

This assumes the provider event reports the recipient address. If a provider
does not, the recorded `pmuid` is used as the correlation key instead. The
Sweego event payload has not yet been inspected, so this remains an assumption
rather than an established fact.

The webhook adapter does not need to understand the complete email workflow.

The progress row is the durable correlation point between the original submission and the asynchronous provider event.

Provider-specific event terminology is translated into these generic events.

---

# 14. Idempotency

The idempotency token belongs to the logical Send transaction. It is the primary
key of its request row and is stored in every related recipient progress row.

The durable layer must ensure that re-execution of a Send sequence does not create duplicate progress records for the same:

```text
(idempotency_token, recipient)
```

pair.

Re-execution with the same token is idempotent only when the complete immutable
request and recipient set match the existing durable records. A conflicting
reuse of the token is rejected.

The database primary key provides the durable identity of the recipient's progress record.

The message ID provides a separate stable identity for the operation, used with
the recipient address by provider callbacks.

---

# 15. Durable Recovery Model

The recovery design exists because external email submission cannot provide an exactly-once guarantee.

The sequence therefore treats an unsuccessful provider call as ambiguous.

The safe assumption is:

```text
provider may have received the request
```

rather than:

```text
provider definitely did not receive the request
```

The resulting workflow is:

```text
Pending
   │
   ├── accepted + PMUID ──────────────► Accepted
   │
   └── error / uncertain
             │
             ▼
         Recovery
             │
             ▼
      RecoveryWaiting
             │
             ▼
          Verify
          /     \
       found   absent
         │        │
         ▼        ▼
      Accepted  Pending
                   │
                   └── may submit again
```

This prevents an ambiguous provider response from immediately causing a duplicate submission.

The provider's own log is given an opportunity to establish whether the original request succeeded.

---

# 16. Separation of Concerns

The resulting architecture is:

```text
Application
    │
    │ Truth / Cell creation
    ▼
Durable Email Delivery
    │
    ├── email_requests
    ├── email_progress
    │
    ├── Cellar Sequence
    │
    ├── EmailClient
    │
    └── EmailVerifier
             │
             ▼
       External Provider
```

The durable layer owns:

* progress;
* state transitions;
* persistence;
* Cellar integration;
* recovery;
* correlation.

The raw provider client owns:

* provider request construction;
* provider API interaction;
* provider response mapping;
* provider-specific verification.

Neither layer takes ownership of responsibilities belonging to the other.

---

# 17. Resulting Guarantees

The design provides the following guarantees:

1. Every recipient has an independently durable progress record.
2. Every recipient is identified by a stable correlation ID and recipient address.
3. A provider submission is never blindly retried immediately after an ambiguous result.
4. Provider logs are used to resolve ambiguous submissions.
5. Provider acceptance is distinguished from eventual delivery.
6. Delivery success and failure are represented independently of provider-specific terminology.
7. Cellar provides durable scheduling and atomic creation of follow-up work.
8. Raw email clients remain independently testable and contain no Cellar dependencies.
9. The request and progress tables together contain the information required to reconstruct a pending submission.
10. The application does not need to know which provider endpoint implements a logical Send operation.

---

# 18. Progress Query API

## 18.1 Purpose

The durable layer exposes a read-only query surface over the progress records so
that an operator interface can display outstanding work, and so that an
individual recipient can be shown the state of the email addressed to them.

The query surface exists for reporting. It is not a source of truth for
application email configuration, and it is not part of any workflow. Nothing in
the Send sequence consults it.

## 18.2 Interface

```go
type ProgressFilter struct {
    IdempotencyToken string
    Recipient        string
    States           []string
    Limit            int
    Offset           int
}

type ProgressRow struct {
    IdempotencyToken string
    Recipient        string
    RecipientName    string
    State            string
    PMUID            string
    SubmittedAt      *time.Time
    Subject          string
    SenderEmail      string
    SenderName       string
    TemplateID       string
}

func (s *Store) QueryProgress(context.Context, ProgressFilter) ([]ProgressRow, error)

type EventRow struct {
    IdempotencyToken string
    Recipient        string
    Event            DeliveryEvent
    RecordedAt       time.Time
}

func (s *Store) QueryEvents(context.Context, ProgressFilter) ([]EventRow, error)
```

A `ProgressRow` is one recipient of one logical Send operation. The request
fields are carried alongside the progress fields so that a caller can label a
row without issuing a second query. Request payload is deliberately excluded:
`text`, `variables`, and `headers` are submission material rather than
reporting material.

`PMUID` is empty while the provider message identifier is NULL. `SubmittedAt` is
nil before the first submission attempt.

`QueryProgress` exposes all new states through `State` and `States` filtering.
`QueryEvents` returns event history using the same token, recipient, limit, and
offset filters; `States` is not applicable to history and must be empty. Events
are ordered by insertion ID, so repeated opens and clicks remain visible.
An index on `(idempotency_token, recipient, id)` supports recipient-history
lookups; the insertion ID supplies global history ordering.

## 18.3 Filter semantics

A zero-value filter selects every progress row. This is the operator view of
overall outstanding state.

Each populated field narrows the selection, and populated fields combine
conjunctively:

* `IdempotencyToken` restricts the result to one logical Send operation.
* `Recipient` restricts the result to one recipient address. The recipient
  address is the only identity the durable layer holds; it carries no account
  identifier of its own.
* `States` restricts the result to the listed states. An empty slice imposes no
  restriction.

Filter values are not validated. A state the durable layer never writes is not
an error; it simply matches nothing. The query surface holds no policy.

## 18.4 Ordering and pagination

Results are ordered by `idempotency_token` then `recipient`. This is the primary
key order of the progress table, so it is total and stable across calls.

`Limit`, when positive, bounds the number of rows returned. `Offset`, when
positive, skips that many rows of the ordered result. Because the ordering is
total, successive pages of an unchanged table are disjoint and exhaustive.

A non-positive `Limit` imposes no bound, and may be combined with a positive
`Offset`.

## 18.5 Execution

The query runs outside any transaction and takes no part in Cellar's
application transaction. It reflects committed state at the moment it runs, and
a caller must not assume that two successive queries observe the same table.

All filter values are bound as query parameters. The query text is fixed apart
from the number of parameter placeholders required by `States`.

## 18.6 Indexes

The progress table carries indexes on `recipient` and on `state`, because the
query surface selects on those columns without an `idempotency_token` prefix and
so cannot use the primary key index.
