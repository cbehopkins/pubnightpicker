# Durable email delivery

This module provides durable email submission and recovery using Cellar.

## Current package

The root `durableemail` package exposes `NewStore(*sql.DB)`. The caller owns the
database connection and its lifecycle. Initialisation creates:

- `email_requests`, containing immutable operation-wide request data;
- `email_progress`, containing recipient-specific request data and progress.

`NewSendSequence` builds the Setup → Recovery → Post sequence. Call
`Register(runtime, store, client, verifier)` to bind every durable email
handler, including the recovery Fanout, before starting Cellar. Invoke
`store.RecoverSubmissions(ctx)` before `Cellar.Start` on each application
startup, while no email workers are running. This marks interrupted submissions
for verification before any recipient can be submitted again. The normative
design is in `docs/overview.md`.

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
