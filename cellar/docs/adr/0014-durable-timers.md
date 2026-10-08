# ADR: Durable named timers

**Status:** Accepted
**Date:** 2026-08-21

## Context

Applications need recurring work that survives process restarts. A timer callback is
application code and cannot itself be persisted, but its identity and next deadline can
be represented by a Cell.

## Decision

Cellar provides a `Timer` component with a stable `HandlerName`, recurrence
configuration, and process-local callback.

Applications create a timer definition and register it on every startup:

```go
timer, err := cellar.NewTimer("reports.refresh", cellar.TimerConfig{
    Interval: 5 * time.Minute,
    Mode:     cellar.TimerFixedDelay,
}, refreshReports)
if err != nil {
    return err
}
if err := timer.Register(runtime); err != nil {
    return err
}
```

Registration reconnects persisted timer work to application code. It does not create
another occurrence. `Schedule` is called only to create the timer initially:

```go
if _, err := timer.Schedule(runtime); err != nil {
    return err
}
```

### Identity

A timer name identifies exactly one active durable timer. Cellar derives a stable Cell
ID from that name. Scheduling the same name while it remains active returns
`ErrTimerAlreadyExists`.

Two independent timers must use different names. Registering duplicate timer names in
one runtime also returns `ErrTimerAlreadyExists`.

### Persistence

The interval, mode, and next deadline are persisted. The callback is process-local and
must be registered under the same name before each runtime start.

After a timer has been scheduled, its persisted configuration is authoritative. The
configuration supplied while reconstructing its registration is used only if that timer
definition later schedules a new timer.

### Recurrence modes

`TimerFixedDelay` schedules the next run one interval after callback completion.

`TimerFixedRate` schedules against the previous deadline. If one or more deadlines were
missed, Cellar coalesces them and schedules the first cadence boundary strictly after
the current time. It does not execute every missed tick.

Successful callbacks return the same Cell to `READY` with its next deadline. The Cell
therefore keeps the same durable identity across executions.

### Callback errors

The callback owns handling and reporting application errors. Returning a non-nil error
instructs the timer wrapper to complete the Cell without replacement, permanently
deleting the timer. The error is not surfaced as a Cellar runtime failure.

## Consequences

* recurring work survives process and database reopening;
* timer names are unique durable identities;
* registration and initial scheduling remain separate operations;
* fixed-delay and fixed-rate behaviour are explicit;
* applications remain responsible for callback error handling and idempotency; and
* scheduling can be repeated after a timer has been deleted or cancelled.
