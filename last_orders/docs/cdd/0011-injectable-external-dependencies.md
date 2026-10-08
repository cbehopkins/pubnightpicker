# CDD: Injectable External Dependencies

**Status:** Proposed
**Date:** 2026-09-22
**Related ADR:** [0006-startup.md](../adr/0006-startup.md), [0008-app-structure.md](../adr/0008-app-structure.md)

---

## 1. Purpose

Allow `app.New` to be constructed with explicitly supplied collaborators so that the
application test bench exercises the real wiring against mocks, rather than against a
live Firestore project or emulator.

The governing rule is that a missing collaborator must **fail loudly at construction**.
Nothing in this document may introduce a code path where the application silently starts
with a degraded or absent dependency.

---

## 2. Background

`app.New` builds every Firestore-backed collaborator inside a single
`if cfg.EnableFirestore { ... }` block. When Firestore is disabled those collaborators
are left nil, but the code that consumes them runs unconditionally.

Three findings motivated this work:

1. Until 2026-09-22, `app.New` dereferenced a nil `*recurrence.Service` via
   `recurrenceService.Location()`. Every test in the `app` package panicked. This has
   been fixed by requiring the service explicitly (§4.1), which converts the panic into
   a clear error but does not make the tests runnable.
2. The recurrence service is only one of **five** Firestore-bound dependencies. Mocking
   recurrence alone moves the failure a few lines down: `newpolls`, `completedpolls` and
   `eventvenues` all reject a nil client at construction, and the autocomplete handlers
   hold a raw `*firestore.Client`.
3. `components/venuecache` and `components/notificationprofile` already implement the
   target shape — a `Source` interface with a `FirestoreSource` implementation. Most of
   this work is applying an existing in-repo pattern to the components that lack it.

There is also an existing precedent for the injection mechanism itself:
`Config.IdempotencyRemote` is already an optional field which, when supplied, replaces
the Firestore-backed construction. This CDD generalises that precedent.

---

## 3. Scope

### In scope

* Consumer-side interfaces for the recurrence service.
* `Source` extraction for `newpolls`, `completedpolls` and `eventvenues`.
* Removing the raw `*firestore.Client` from the autocomplete handlers.
* Individual optional `Config` fields for each collaborator.
* Test doubles, and conversion of the `app` package tests to use them.

### Out of scope

* Changing any business logic in `components/recurrence`, the poll listeners, or
  autocomplete. This is a dependency-seam change only.
* Changing Cellar's behaviour on handler failure. The current rule — any handler error
  stops the scheduler — is retained deliberately; see §7.
* The `venuecache` and `notificationprofile` components, which already conform.

---

## 4. Design

### 4.1 Required collaborators fail at construction

Already implemented:

```go
if recurrenceService == nil {
    return nil, fmt.Errorf("recurrence service is required: enable firestore")
}
```

Each collaborator introduced below gains the equivalent guard. A nil collaborator is
never tolerated, never defaulted to a no-op, and never skipped with a conditional
registration.

### 4.2 Interfaces are declared by the consumer

Interfaces live in the package that consumes them, not alongside the implementation.
`*recurrence.Service` continues to satisfy them without modification; the component
itself is not edited.

The full recurrence surface is four methods across four consumers:

| Method | Consumer |
|---|---|
| `Location() *time.Location` | `app.New`, constructing `EvaluateEventVenueHandler` |
| `Today() time.Time` | `database/listeners/eventvenues` |
| `AdvanceStaleEvent(ctx, eventID) error` | `plugins/recurrence.StaleEventHandler` |
| `CreateEventPoll(ctx, eventID, occurrenceDate) error` | `plugins/recurrence.CreateEventPollHandler` |

Each consumer declares only the methods it uses, rather than sharing one wide interface.

### 4.3 Listeners depend on a Source, not a client

`newpolls`, `completedpolls` and `eventvenues` follow the shape already established by
`venuecache`:

```go
type Source interface { /* only what the listener needs */ }

type FirestoreSource struct { client *firestore.Client }
```

`eventvenues` is the most involved: it owns both a query
(`Collection("pubs").Where("venueType", "==", "event")`) and a `Snapshots` stream, so
query construction moves behind the interface. `venuecache.Watch` is the reference
implementation for the stream half.

### 4.4 Injection via individual optional Config fields

Collaborators are supplied as discrete optional fields on `Config`, mirroring the
existing `Config.IdempotencyRemote`:

```go
type Config struct {
    // ...
    IdempotencyRemote         firebaseidempotency.Remote  // existing precedent
    RecurrenceService         RecurrenceService
    EventVenueSource          eventvenues.Source
    NewPollSource             newpolls.Source
    CompletedPollSource       completedpolls.Source
    AutocompleteSource        autocomplete.Source
}
```

Resolution order per field, with no fallback beyond it:

1. Use the supplied value if non-nil.
2. Otherwise construct the Firestore-backed implementation, if Firestore is enabled.
3. Otherwise return an error naming the missing collaborator.

A grouped dependencies struct was considered and rejected in favour of explicitness at
each call site.

---

## 5. Work Phases

Phases 1–3 land together. Phase 4 follows. Nothing is unblocked until Phase 5.

**Phase 1 — Seams (single change set)**

1. Declare the recurrence interfaces at their four consumers.
2. Extract `Source` for `newpolls` and `completedpolls`.
3. Extract `Source` for `eventvenues`.

**Phase 2 — Autocomplete.** Replace the `*firestore.Client` field on the discovery,
candidate, close and ambiguous handlers with a narrow source interface. These handlers
currently fail at dispatch rather than construction, which is why they are sequenced
after Phase 1.

**Phase 3 — Config fields.** Add the fields from §4.4 and the §4.1 guards.

**Phase 4 — Test doubles.** New `...test` packages following the existing
`firebaseidempotencytest` convention. Fakes fail loudly on unexpected calls; they do not
return zero values silently.

**Phase 5 — Convert the `app` tests** to inject fakes, and add a thin
`app_emulator_test.go` covering the real Firestore wiring, skipped when
`FIRESTORE_EMULATOR_HOST` is unset — the convention already used by `recurrence`,
`notificationprofile` and `eventvenues`.

---

## 6. Testing

The `app` package keeps two tiers:

* **Mocked wiring** (default, no external services): construction, registration,
  lifecycle, idempotency and failure-propagation behaviour.
* **Emulator wiring** (opt-in via `FIRESTORE_EMULATOR_HOST`): that the Firestore-backed
  implementations satisfy the same interfaces in practice.

`app/run_failure_test.go` — which asserts that a single handler failure stops the whole
runtime — becomes runnable on completion of Phase 5. Until then the `app` package fails
at construction with `recurrence service is required: enable firestore`.

---

## 7. Notes

The rule that any handler error stops the scheduler is a deliberate prototype
posture — errors are not expected, so the application stops when one appears. It is
recorded here only so that this CDD is not read as endorsing graceful degradation.
Revisiting it (for example, distinguishing transient backend failures via `Retry` with a
deadline from genuine faults) is separate work and needs its own ADR.

---

## 8. Open Questions

* Should `eventvenues.Source` expose the venue query as a parameter, or keep the
  `venueType == "event"` predicate internal to the Firestore implementation?
* Do the poll listeners share one `Source` interface, or keep one each? They observe
  different collections but have near-identical stream semantics.
