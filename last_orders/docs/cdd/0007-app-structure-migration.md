# CDD: Migration to ADR 0008 Application Structure

## 1. Purpose

Migrate the current `internal/lastorders` layout onto the package structure decided in
[0008-app-structure.md](../adr/0008-app-structure.md), remove the example
scaffolding now that real listeners and services exist, and adopt Cellar's Fanout,
Sequence, and durable Timer primitives where they replace hand-rolled equivalents.

---

## 2. Scope

### In scope

* Renaming/moving packages onto the `app`, `components`, `database`, `services`,
  `plugins`, `endpoints` shape.
* Deleting the example listener, example handlers, and the `counter` component that
  only existed to support the example.
* Re-implementing `firebaseidempotency` (and introducing a new local-only
  `idempotency` component) as Cellar Sequences per the revised
  [0001-idempotency.md](0001-idempotency.md), replacing the Pending/Push/Present
  state table and hand-rolled fanout with genuine `cellar.AddSequence` +
  `cellar.Fanout` usage.
* Replacing the event-venue re-evaluation `time.Ticker` loop with a registered
  `cellar.Timer`.
* Updating `app.go`, `main.go`, and tests for all of the above.

### Out of scope

* Changing recurrence business logic (`components/recurrence` internals).
* Introducing `endpoints` (no HTTP endpoints exist yet, e.g. Sweego webhook) —
  the package will be created empty/deferred until that work starts.
* Converting `recurrence.Service.CreateEventPoll` into a multi-step Cellar Sequence
  (flagged as a future candidate, see §6).

> **Revision note (2026-08-27):** §5 was rewritten after
> [0001-idempotency.md](0001-idempotency.md) was updated to fix a spec issue in the
> original design — idempotency state was being duplicated into an explicit
> Pending/Pushed/Present table when the Cell Sequence's own step cursor should be
> the processing state machine, and Fact delivery was being hand-assembled instead
> of using a genuine Cellar Fanout Cell. This is now a required part of the
> migration, not an optional "adopt Fanout where convenient" nicety, and it is
> larger in scope than originally estimated (§5).

---

## 3. Target Layout

```text
internal/lastorders/
    app/
        app.go                       (unchanged responsibility, updated imports)

    components/
        firebaseidempotency/         (unchanged, minus hand-rolled fanout)
        recurrence/                  (unchanged)

    database/
        listeners/
            eventvenues/             (was listeners/eventvenues)
            newpolls/                (was listeners/newpolls)
            completedpolls/          (was listeners/completedpolls)

    plugins/
        recurrence/                  (was handlers/recurrence.go)
        polls/                       (was handlers/polls.go)

    services/
        recurrence/                  stays fronted by components/recurrence;
                                      no separate services package needed yet since
                                      the only "unit of work" handlers today are the
                                      plugin-level handlers themselves — see §5 note.
```

Removed entirely:

```text
internal/lastorders/listeners/example/
internal/lastorders/handlers/example.go
internal/lastorders/components/counter/
```

### Note on `handlers/` → `plugins/`

Per ADR 0008, a **plugin** answers "when this Truth occurs, which units of work
should be triggered?" and a **service** answers "how is that unit of work performed?"
Today, `handlers/polls.go` and `handlers/recurrence.go` are both: they are registered
directly against Cellar `HandlerName`s and contain the (currently trivial) unit of
work themselves. There is no independent business logic sitting behind them the way
`recurrence.Service` sits behind the recurrence plugin.

Rather than invent an empty `services` package purely to satisfy the shape, this
migration:

* moves `handlers/polls.go` → `plugins/polls/polls.go` (it only logs today — it is
  pure connectivity plus a no-op unit of work);
* moves `handlers/recurrence.go` → `plugins/recurrence/recurrence.go` (it *does*
  delegate real work to `components/recurrence.Service`, which already plays the
  services role).

If poll handling grows real unit-of-work behaviour (e.g. notifying, archiving), that
behaviour should move into a `services/polls` package at that time rather than being
added to the plugin handler. This is called out so the distinction doesn't erode.

### Database package

`database/listeners/*` replaces `listeners/*` (minus `example`). No caches exist yet,
so `database/caches/` is not created until one is needed — ADR 0008 says the
structure should make responsibilities visible, not impose empty directories
speculatively.

---

## 4. Removing the Example Listener

Delete:

* `internal/lastorders/listeners/example/producer.go`
* `internal/lastorders/handlers/example.go`
* `internal/lastorders/components/counter/store.go` (only consumer is the example)

Update:

* `app.go`: drop `exampleListener` field, `EnableExampleListener` config, the
  `counter.Store`/`counter.New` wiring, and the `HandlerExampleIncrement` /
  `HandlerExampleFanout` registrations.
* `main.go`: drop `-example-listener` flag and the closing `CounterValue` log line.
* `app_integration_test.go`: remove/replace the tests that exercise the example
  handlers directly (`TestDatabaseOwnershipSingleSQLiteForAppAndCellar`,
  `TestApplicationWorkAtomicWithCellCompletionRollbackOnFailure`,
  `TestCellFanoutCreatesMultipleReplacementCellsAtomically`). The first two need a
  substitute real handler+payload (e.g. a minimal fixture handler local to the test
  file) to keep covering "shared DB" and "ApplicationWork rollback" behaviour; the
  fanout test is superseded by new coverage of the genuine Fanout Cell introduced
  by the idempotency redesign (§5.3).

---

## 5. Idempotency Redesign: Sequence-Based State Machine + Genuine Fanout

This section supersedes the original plan's narrower "adopt `cellar.Fanout` in
`CheckHandler`" note. [0001-idempotency.md](0001-idempotency.md) was revised to fix
two problems with the original spec:

1. **Duplicated state machine.** The `firebase_idempotency_records` table's
   `PENDING`/`PUSHED`/`PRESENT` `state` column re-implements a processing stage that
   the Cellar Cell Sequence's own step cursor already represents durably and
   atomically. Carrying both invites the two getting out of sync.
2. **Hand-rolled fanout.** `CheckHandler` builds `[]cellar.CellRequest` manually from
   `payload.Fanout` (`fanoutCellRequests`) instead of using a real, registered
   `cellar.Fanout[T]`/Fanout Cell as the delivery mechanism for a Fact.

The corrected architecture (§4–§14 of the CDD):

```text
Observation → Idempotency Sequence → (duplicate: Kill) | (new: create Fanout Cell)
                                                                 │
                                                                 ▼
                                                          registered Fact handlers
```

### 5.1 Local idempotency component (new)

A new component (name TBD, e.g. `components/idempotency`) implements the **local**
variant for observations where the local Base DB is authoritative (timers,
housekeeping, backend-internal events — see §6/§21 of the CDD). It is a 1-step
Cellar Sequence:

* **Step 1 — Check and Establish.** Look up `(component, key)` in a local key table
  (existence-only, no state enum). If present → `cellar.Kill{}`. If absent → one
  atomic `Complete` that inserts the key, creates the Fact's Fanout Cell (via
  `NewCells`), and completes the step.

This is what `firebaseidempotency` should *not* duplicate — it is a separate,
simpler component reused wherever remote backing isn't needed.

### 5.2 `components/firebaseidempotency` becomes a 3-step Sequence

Replace the current independent `HandlerPending` / `HandlerPush` / `HandlerCheck`
Cells — each created via a manual `Complete{NewCells: [...]}` chain — with a single
`cellar.AddSequence` of three steps mirroring §8–§11 of the CDD:

| Step | Handler name (indicative) | Behaviour |
|---|---|---|
| 1 | `firebase.idempotency.check` | Check local key: present → `Kill{}`. Check remote (`Remote.HasKey`): present → `Kill{ApplicationWork: [cache local key]}` (atomically caches and terminates — no Fanout). Both absent → `Complete{ApplicationWork: [insert local key]}` (advance to step 2). |
| 2 | `firebase.idempotency.populate_remote` | `Remote.CreateKey`; on success, complete/advance to step 3. Must tolerate being retried against the same identity (already required of `Remote` today). Only reached for a genuinely new claim, so it always runs unconditionally. |
| 3 | `firebase.idempotency.emit_fact` | `Complete{NewCells: [fanout cell]}` creating the Fact's Fanout Cell, then completing the sequence. Only reached for a genuinely new claim, so it always runs unconditionally. |

Schema change: `firebase_idempotency_records` drops its `state` column entirely and
becomes an existence-only local cache table (`listener`, `event_key`,
`updated_at`) — matching §16 ("the local key identifies the idempotency identity;
the Cell Sequence identifies the current processing stage"). `Store.CurrentState`,
`StatePending`/`StatePushed`/`StatePresent`, `CreateOrRefreshPending`,
`MarkPushedUnlessPresent`, `TransitionPendingToPresent`, and
`TransitionPushedToPresentWork` are all removed/replaced by a single existence
check + insert.

**Resolved (2026-08-27):** this is a literal `cellar.AddSequence` of three steps.
Step 1 returns `cellar.Kill{}` to terminate early on a duplicate. A `Kill{}`
mid-sequence is not "branching" in the ADR 0016 sense — there remains exactly one
declared, ordered path through the Sequence's steps; `Kill{}` just means later
steps never execute.

**Implementation note (2026-08-27):** the first pass of this step incorrectly
introduced an `observed_only` flag on the local cache row as a workaround, because
Cellar's `Kill` did not yet support `ApplicationWork`/`NewCells`, so Step 1 could
not atomically "cache the key and terminate" for the "remote already established"
branch in one result. That workaround was rejected — Cellar was instead extended
so `Kill` carries `ApplicationWork`/`NewCells` like `Complete`, and the flag was
removed. Steps 2 and 3 no longer need to check anything: reaching them at all
already means Step 1 determined the claim was genuinely new.

### 5.3 Fact registration / Fanout Cell

Per §12 of the CDD, plugins register interest in **Facts**, not directly in Cellar
`HandlerName`s, and must not need to know which idempotency variant produced the
Fact. This implies a small Fact registry sitting above Cellar's `cellar.Fanout[T]`:

* Something like `facts.Register(FactNewPoll, handlerA, handlerB, ...)` at startup,
  building the `[]cellar.FanoutTarget` list once.
* The idempotency Step 3 (or local Step 1's success branch) looks up the Fact's
  registered targets and emits a genuine Fanout Cell — either via a registered
  `cellar.Fanout[FactPayload]` per Fact name, or via a shared generic
  `cellar.Fanout[GenericFactEnvelope]` keyed by Fact name at expansion time. The
  former is more idiomatic Cellar usage (named, typed, registered per Fact) and is
  the recommended approach; needs one `cellar.NewFanout` + `.Register` call per Fact
  type at startup, mirroring how handlers are registered today in `app.go`.
* This likely wants its own small package (`components/facts` or similar) rather
  than living inside `firebaseidempotency`, since local idempotency (§5.1) needs the
  same registry.

### 5.4 Listeners are explicitly out of scope for this migration

`newpolls`, `completedpolls`, and `eventvenues` listeners currently build a
`firebaseidempotency.PendingPayload{..., Fanout: []FanoutTarget{...}}` themselves.
Under the fully-realised CDD, that fanout-target decision belongs to Fact
registration (§5.3) rather than the listener. **Decision (2026-08-27): deferred.**
To keep this migration's scope down to "get the shape correct", the three listeners
are left exactly as they are — including their simple log-message handlers — and
are not rewired to the new Sequence/Fact-registry shape in this pass. Wiring
listeners (and real plugin handlers) up to the redesigned idempotency/Fact
infrastructure is follow-up work, tracked separately, once §5.1–§5.3 land.

**Non-candidates found:** the `eventvenues` listener's `evaluate()` only ever
produces one Fact per venue per tick (either stale-event or event-due) — the
multi-target expansion happens entirely at the Fact-registration layer (§5.3), not
in the listener. This remains true whenever the wiring work does happen.

---

## 6. Adopting Sequence

Aside from the idempotency redesign in §5 (which has its own open question about
whether it can be a literal `AddSequence` — see §5.2's caveat), no other current
handler chain fits `Cellar.AddSequence` today, because:

* `recurrence.Service.CreateEventPoll` performs multiple Firestore writes (poll,
  vote/attendance docs, audit) inside one Go function call today, not as separate
  Cellar handlers, so there's nothing to sequence yet.

**Recommendation:** flag `CreateEventPoll` as the natural first Sequence candidate
(`recurrence.create_poll` → `recurrence.create_vote_doc` →
`recurrence.create_attendance_doc` → `recurrence.audit`), but treat that as a
follow-up CDD/ADR-sized change to `components/recurrence`, not part of this
structural migration, since it changes business-logic transaction boundaries and
needs its own test plan.

---

## 7. Adopting Durable Timers

**Target:** `database/listeners/eventvenues` re-evaluation loop.

Today `Listener.reevaluate` runs an in-process `time.NewTicker(l.interval)` goroutine
that is not durable — a restart resets the next tick to a full interval away, and the
interval is a runtime `Config` field, not persisted.

Plan:

1. Register a `cellar.Timer` named e.g. `eventvenues.reevaluate` with
   `TimerConfig{Interval: cfg.ReevaluateInterval, Mode: cellar.TimerFixedRate}` (fixed
   rate matches the current "run roughly every N" intent and coalesces missed ticks
   after downtime, which is strictly better than the current ticker's behaviour of
   just restarting the interval).
2. Callback body is today's `reevaluate` tick body (list venues, call `l.evaluate`
   for each), returning `error` only for conditions that should delete the timer
   (none currently — log-and-continue as today, return `nil` always so the timer
   keeps recurring; matches current "log and continue" behaviour on
   `ListEventVenues` failure).
3. `Listener.Start` stops spawning the `reevaluate` goroutine; instead `app.go` calls
   `timer.Register(cellarRuntime)` at startup (every run) and `timer.Schedule(...)`
   once, tolerating `ErrTimerAlreadyExists` on subsequent starts (same pattern the
   watch-based listeners don't need, since they aren't durable state — only the
   timer is).
4. `Listener.watch` (the Firestore snapshot loop) is unaffected — it stays a
   plain goroutine since Firestore watch state is not itself meant to be durable
   across restarts (a fresh snapshot listener resends current state on reconnect).

Consequence: `eventvenues.Config.ReevaluateInterval` becomes the *initial* schedule
interval only; once persisted, Cellar's timer is authoritative (per ADR 0014,
"persisted configuration is authoritative"), so changing the flag after the timer
already exists has no effect without deleting/recreating the timer cell. Worth a
one-line note in `main.go`'s flag help text.

---

## 8. Suggested Execution Order

1. Delete example code + adjust `app.go`/`main.go`/tests (§4). Gets the codebase to
   a clean baseline before moving files around.
2. Directory rename `listeners/` → `database/listeners/`, `handlers/*.go` →
   `plugins/<name>/*.go` (§3). Pure move + import fixups, no behaviour change.
3. Timer adoption for `eventvenues` (§7). Independent of the idempotency rework,
   safe to land on its own.
4. Idempotency redesign (§5), a literal `AddSequence` with early `Kill{}`
   termination (§5.2):
   1. introduce the local-only `idempotency` component (§5.1);
   2. introduce the Fact registry (§5.3);
   3. rework `firebaseidempotency` into the Check/Populate-Remote/Emit-Fact
      `AddSequence` and drop the `state` column (§5.2).
   Listener wiring (§5.4) is explicitly deferred — `newpolls`, `completedpolls`,
   and `eventvenues` keep their current simple log-message handlers and are not
   touched in this migration beyond the directory move in step 2.
5. Leave the `CreateEventPoll` Sequence (§6) as a documented follow-up, not
   executed now.

Steps 1–2 are mechanical and low-risk. Step 3 changes runtime behaviour in one
contained listener. Step 4 is the largest piece of this migration — it touches the
database schema, `app.go` registration, and the bulk of the existing idempotency
test suite (`remote_firestore_emulator_test.go`, `app_integration_test.go`'s
idempotency tests) — and probably warrants landing as its own reviewable unit
rather than in the same change as the directory move. Re-run `eligibility_test.go` and
`service_emulator_test.go` after step 3 regardless.

---

## 9. Open Questions

Both prior open questions are resolved (2026-08-27):

* The idempotency Sequence is a literal `AddSequence` with early `Kill{}` (§5.2).
* Listener wiring to Facts/the new idempotency shape is deferred out of this
  migration's scope (§5.4).

Remaining, lower-stakes questions:

* Should `plugins/polls` be split into `plugins/polls/new` and `plugins/polls/completed`
  to mirror the two listeners, or stay one file with two handlers as today? Leaning
  towards keeping one file — the split listeners are Firestore-query differences, not
  differences in the unit of work.
* Does `services/` need to exist as an empty placeholder package now, or only once a
  real service (e.g. email/notification) is added? Leaning towards not creating it
  speculatively, consistent with ADR 0008's "avoid unnecessarily rigid hierarchy".
