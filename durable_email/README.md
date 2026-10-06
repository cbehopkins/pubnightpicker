# Durable email delivery

This module provides durable email submission and recovery using Cellar.

## Current package

The root `durableemail` package exposes `NewStore(*sql.DB)`. The caller owns the
database connection and its lifecycle. Initialisation creates:

- `email_requests`, containing immutable operation-wide request data;
- `email_progress`, containing recipient-specific request data and progress;
- `email_events`, containing delivery and tracking event history.

`NewSendSequence` builds the Setup → Recovery → Post sequence. Call
`Register(runtime, store, client, verifier)` to bind every durable email
handler, including the recovery Fanout, before starting Cellar. Invoke
`store.RecoverSubmissions(ctx)` before `Cellar.Start` on each application
startup, while no email workers are running. This marks interrupted submissions
for verification before any recipient can be submitted again. The normative
design is in `docs/overview.md`.

## Best-effort submissions

`SendRequest.Metadata` is immutable, application-owned `map[string]string`
context exposed to the submission guard/policy. It is stored as JSON in
`email_requests.metadata`, preserved through recovery and never sent as provider
headers or template variables. Changing it under an existing idempotency token
is rejected. Initialisation adds this column with an empty-object default to
older SQLite databases; legacy queued requests have no metadata.

`RegisterOptions.SubmissionPolicy` can select a provider client and best-effort
handling once per attempt. Without it, existing durable submission and recovery
behaviour is unchanged. Delays and errors fail closed before submission.

Best-effort handling bypasses `SubmissionGuard` and atomically marks all pending
recipients `Accepted` before contacting the provider. Their `suppressed:` PMUIDs
are synthetic handling identifiers, not proof of provider acceptance or delivery.
Provider failures, refusals and malformed results are logged through the optional
`Logger` and never retried. A restart cannot replay a precommitted best-effort
attempt live; the diagnostic send may instead be lost. Local persistence failures
are not ignored. See `docs/overview.md` for the policy contract.

## Delivery events

An authenticated Sweego webhook adapter can call
`store.RecordEvent(ctx, messageID, recipient, event)` with one of the exported
`EventSent`, `EventDelivered`, `EventSoftBounce`, `EventHardBounce`,
`EventProxyOpen`, `EventHumanOpen`, `EventClick`, or `EventSpamComplaint` values.
The durable module records the event and advances the recipient status without
overwriting a stronger status with a late notification. It does not parse or
authenticate HTTP webhooks; list-unsubscribe and Mailtrap webhooks are outside
this contract.

Use `store.QueryProgress(ctx, ProgressFilter{States: []string{StateSpamComplaint}})`
for current status, or `store.QueryEvents(ctx, ProgressFilter{IdempotencyToken: token})`
for event history including repeated opens and clicks. `Refused` is reserved
for a definitive permanent refusal at Post; the current provider-neutral
client result does not classify permanent refusals, so Post does not set it yet.

## Sweego

The same handlers work with Sweego's sender and logs verifier. Construct both
from the provider client and pass them to `Register`:

```go
provider := sweego.NewClient(baseURL, token, timeout).WithSendOptions(sweego.SendOptions{
	Provider: providerName,
})
verifier := logs.NewVerifier(logs.NewClient(provider), tolerance)
if err := durableemail.Register(runtime, store, provider, verifier); err != nil {
	return err
}
```

Import `email_clients/clients/sweego` and `email_clients/clients/sweego/logs`.
Use your Sweego API token and provider name (for example, from `SWEEGO_TOKEN`
and `SWEEGO_PROVIDER`); set `baseURL` to `https://api.sweego.io` for the
production API.
Choose a verification tolerance that covers the expected log-ingestion delay.
Run `store.RecoverSubmissions(ctx)` before starting Cellar as described above.

## Mailtrap

Mailtrap's sender and production-log verifier implement the same interfaces:

```go
sdkClient, err := sdk.NewClient(token, sdk.WithHTTPClient(&http.Client{Timeout: 15 * time.Second}))
if err != nil {
	return err
}
provider, err := mailtrap.NewClient(sdkClient)
if err != nil {
	return err
}
provider = provider.WithSendOptions(mailtrap.SendOptions{Category: "Pub notification"})
if err := durableemail.Register(runtime, store, provider, mailtrap.NewVerifier(provider, 5*time.Minute)); err != nil {
	return err
}
```

Import `github.com/mailtrap/mailtrap-go` as `sdk` and
`email_clients/clients/mailtrap`. Provide a production Mailtrap token with
access to email logs; sandbox logs cannot be used for recovery. Mailtrap accepts
at most 500 recipients per Send, so submit larger groups as separate durable
operations. Raw messages require both `Subject` and `Text`; hosted templates
require `TemplateID` with empty `Subject` and `Text`. Set the verification
tolerance to cover log ingestion, and call `store.RecoverSubmissions(ctx)` before
starting Cellar. A partial batch error puts the entire batch through recovery:
durable email verifies recipients before any resubmission, including those
whose IDs were returned alongside the error.

## Integration shape

The eventual `last_orders` wrapper will pass its shared `baseStore.DB()` to
`durableemail.NewStore` and register durable email handlers with the existing
Cellar runtime. The reusable module does not import application-internal store
types.

## Commands

```text
go test ./...
go build ./...
```
