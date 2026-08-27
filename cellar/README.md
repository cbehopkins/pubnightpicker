# cellar

Cellar is a lightweight execution runtime for backend services.

It provides a durable store of **Cells**: small, atomic units of work that can
be scheduled for immediate or future execution.

Cellar is designed for systems that need:

- persistent background work
- retries and delayed execution
- idempotent event handling
- simple workflow composition
- process restart recovery

Cellar intentionally avoids distributed systems concerns such as leases,
consensus and competing consumers. Its initial focus is correctness,
simplicity and debuggability for single-process backends.

---

## Motivation

Many backend systems react to changes in business state:

- a poll opens
- a poll closes
- a chat message is sent
- a maintenance timer fires

These **Domain Events** often trigger asynchronous work:

- send emails
- send push notifications
- clean up old data
- generate reminders

Cellar exists to provide a small runtime that executes this work reliably
without coupling business logic to infrastructure concerns.

---

## Core concepts

### Domain Event

A Domain Event is an immutable fact about the business domain.

Examples:

- `PollOpened`
- `PollClosed`
- `ChatMessageSent`

Cellar does not understand Domain Events directly. Domain Events are observed
by application code, which creates Cells.

---

### Cell

A Cell is the fundamental unit of execution.

Examples:

- send a mailing-list email
- send a push notification
- delete expired data

Cells are:

- durable
- atomic
- transient
- retryable
- schedulable

A Cell may create other Cells.

A Cell does not own other Cells.

---

### Handler

Each Cell has one or more ordered steps, and each step has a corresponding Handler.

Handlers contain business logic and receive a typed payload. Cellar binds the handler to a
stable, human-readable name and handles JSON encoding and decoding internally.

```go
type SendEmail struct {
    Recipient string `json:"recipient"`
}

type SendEmailHandler struct{}

func (SendEmailHandler) Handle(ctx context.Context, payload SendEmail) cellar.Result {
    // Application business logic.
    return cellar.Complete{}
}
```

Handlers do not directly manage persistence.

Handlers return instructions to the runtime.

### Ordered sequences

`Add` creates an ordinary one-step Cell. Use `AddSequence` when several handlers must
run in order. Each step may have a different payload type:

```go
cellID, err := c.AddSequence(
    cellar.Step{HandlerName: "order.validate", Payload: ValidateOrder{ID: orderID}},
    cellar.Step{HandlerName: "order.reserve", Payload: ReserveOrder{ID: orderID}},
    cellar.Step{HandlerName: "order.notify", Payload: NotifyOrder{ID: orderID}},
)
```

The Cell keeps a durable `CurrentStep` cursor and retains the same ID across the
sequence. A successful `Complete` advances to the next step; an ordinary `Retry`
repeats the current step; `RetrySequence` resets execution to the first step after
its delay. Child Cells and `ApplicationWork` are committed atomically with the step
transition. A `Kill` removes the current Cell without running later steps, but may
also carry cleanup Cells and `ApplicationWork`; those side effects commit atomically
with the termination. A final `Complete` also removes the Cell.

---

## Usage

Cellar is the composition root for registration, scheduling, execution, and result
application. Applications provide a store, configuration, and typed handlers:

```go
store := cellar.NewMemoryStore(nil)

c := cellar.New(store, cellar.Config{
    PollDelay: 100 * time.Millisecond,
	Workers:   4,
})

if err := c.Register("email.send", SendEmailHandler{}); err != nil {
    return err
}

if _, err := c.Add("email.send", SendEmail{Recipient: "person@example.com"}); err != nil {
    return err
}

if err := c.Start(ctx); err != nil {
    return err
}
```

`Start` closes registration, recovers claimed Cells, and runs until its context is
cancelled or `Stop` is called. Applications do not construct or freeze a registry and
do not construct workers, dispatchers, result appliers, or schedulers.

### Fanout

A Fanout expands one durable Cell into zero or more ordinary child Cells. Each target
has a key that is stable for that parent. Cellar uses the parent ID and target key to
derive the child's opaque ID, so retrying the same expansion cannot silently create a
second copy of a target.

```go
fanout, err := cellar.NewFanout(
    "order.completed",
    cellar.FanoutExpanderFunc[OrderCompleted](
        func(ctx context.Context, parentID cellar.CellID, order OrderCompleted) ([]cellar.FanoutTarget, error) {
            return []cellar.FanoutTarget{
                {
                    Key:         "email",
                    HandlerName: "email.send",
                    Payload:     SendEmail{OrderID: order.ID},
                },
            }, nil
        },
    ),
)
if err != nil {
    return err
}
if err := fanout.Register(c); err != nil {
    return err
}
if _, err := fanout.Add(c, OrderCompleted{ID: "order-42"}); err != nil {
    return err
}
```

All targets are persisted atomically with completion of the parent. Once persisted,
they follow the normal `READY`, `NotBefore`, claim, retry, and recovery rules. See the
tested example in [pkg/cellar/example_test.go](pkg/cellar/example_test.go).

### Durable timers

A named timer runs recurring application work and persists its next deadline:

```go
timer, err := cellar.NewTimer("reports.refresh", cellar.TimerConfig{
    Interval: 5 * time.Minute,
    Mode:     cellar.TimerFixedDelay,
}, func(ctx context.Context) error {
    return refreshReports(ctx)
})
if err != nil {
    return err
}
if err := timer.Register(c); err != nil {
    return err
}
if _, err := timer.Schedule(c); err != nil {
    return err
}
```

Register the same timer name and callback on every process startup. Call `Schedule`
only when creating it initially; scheduling an active name returns
`ErrTimerAlreadyExists`. `TimerFixedDelay` schedules from callback completion, while
`TimerFixedRate` maintains cadence and coalesces missed ticks. A callback that returns
an error cancels and deletes the timer.

---

### Scheduler

The scheduler finds runnable Cells and dispatches them to workers.

A Cell is runnable when:

- its state is `READY`
- its scheduled time has arrived

---

## Cell lifecycle

```text
READY
    ↓
CLAIMED
    ↓
DELETED
```

When a process starts, any claimed Cells are returned to the ready state.

This guarantees recovery after:

- process crashes
- machine restarts
- deployments
- power failures

---

## Retry model

Retries are explicit.

Handlers may request:

- immediate retry
- delayed retry
- abandonment

The runtime itself contains no retry policy.

Business logic decides retry behaviour.

---

## Design principles

Cellar follows several architectural principles:

- Cells are the only execution primitive.
- Business logic and infrastructure are separated.
- The runtime owns persistence and scheduling.
- Handlers are conceptually stateless.
- Distribution is an implementation detail.
- Complexity belongs in workflows, not infrastructure.

---

## Non-goals

Cellar does not currently attempt to provide:

- distributed execution
- exactly-once processing
- leases
- consensus
- workflow orchestration
- competing consumers

These concerns may be introduced in future implementations without changing
the core abstractions.

---

## Status

Cellar is currently experimental.

The API is expected to evolve significantly as the execution model matures.
