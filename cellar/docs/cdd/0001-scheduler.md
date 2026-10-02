# Cellar Scheduler Architecture

## Responsibility

The Scheduler is the component responsible for discovering runnable Cells and transferring ownership of those Cells to Cellar's execution workers.

The Scheduler owns the lifecycle transition:
```text
READY -> CLAIMED
```

The Scheduler owns the decision to claim work. The Store performs the atomic state transition.

The Scheduler does not:

- execute handlers;
- decode payloads;
- apply handler results;
- manage business logic;
- decide retry policy.

Its only responsibility is:

Find work, claim work, dispatch work.

## Relationship to other components

The high-level architecture is:

```text
                     Cellar

                 +-----------+
                 | Scheduler |
                 +-----------+
                      |
                      |
                Claim READY Cell
                      |
                      v
                 +---------+
                 |Capacity |
                 +---------+
                      |
                      |
        +-------------+-------------+
        |                           |
        v                           v

   +---------+                 +---------+
   | Worker  |                 | Worker  |
   +---------+                 +---------+
        |
        |
        v

   Handler execution
```

The Store remains the authority for persisted Cell state.

## Scheduler lifecycle

The Scheduler repeatedly performs:

- ask the Store for a runnable Cell;
- atomically transition that Cell from READY to CLAIMED;
- emit a CellClaimed Notice;
- place the Cell onto the worker queue.

Conceptually:

```go
for {
    cell, ok := store.ClaimNext(now)

    if !ok {
        sleep()
        continue
    }

    notices.CellClaimed(cell)

    queue <- cell
}
```
Notice emission occurs after successful claiming but before dispatch. Failure to emit a Notice does not prevent dispatch.

## CLAIMED semantics

A CLAIMED Cell means:

Cellar has accepted responsibility for this Cell.

It does not mean:

- a Handler has started;
- a Worker has received it;
- execution is currently occurring.

A CLAIMED Cell may be:

- waiting in the worker queue;
- being decoded;
- executing;
- completing;
- recovering from a failure.

This distinction deliberately avoids exposing internal scheduling mechanics through persisted state.

## Worker interaction

Workers consume Cells from the queue.

Workers are not aware of scheduling.

Their responsibility is:

- receive a claimed Cell;
- decode the payload using the registered Codec;
- execute the Handler;
- apply the returned Result;
- emit appropriate Notices.

Workers do not directly modify lifecycle state except through runtime operations.

## Admission model

The Scheduler maintains one in-memory capacity token per configured worker. It reserves
a token before calling `ClaimNext`, so it does not claim substantially more work than
can execute. A token is returned when dispatch finishes.

Capacity is a scheduling hint rather than persisted correctness state. The Store's
atomic `READY -> CLAIMED` transition remains authoritative. The worker count is
configurable and defaults to one.

Future versions may introduce:

- adaptive queueing;
- scheduler wake-up signals;
- smarter NotBefore handling;
- priority queues;
- direct worker capacity scheduling.

These are optimisations, not architectural requirements.

## Idle behaviour

When no runnable Cells exist, the Scheduler sleeps before polling again.

V0 implementation may use simple polling:

```text
poll Store
    |
no work
    |
sleep
    |
poll again
```

Future versions may support wake-up signals.

Examples:

- external listener receives new work;
- Cell is inserted with an earlier NotBefore;
- external trigger indicates queued work exists.

## Worker count

Scheduler concurrency is independent of Cell identity and Handler logic.

The runtime controls worker count.

Conceptually:

```go
type Config struct {
    Workers int
}
```

The initial implementation may default to one worker.

Future implementations may use:

- CPU count;
- workload-specific configuration;
- dynamic scaling.

## Recovery

On startup:

```sql
UPDATE cells
SET state = 'READY'
WHERE state = 'CLAIMED';
```

The Scheduler does not distinguish between:

- process crash;
- machine failure;
- panic;
- graceful shutdown.

All unfinished owned work returns to READY.
## Shutdown semantics
When context is cancelled, the contenxt should already have been propogated to the workers so that workers can begin their own shutdown procedure.

stop claiming new Cells;
allow already claimed Cells to finish according to worker policy;
exit.

Something like:

```
SIGTERM
   |
   v
Cellar context cancelled
   |
   +--> Scheduler stops claiming
   |
   +--> Workers receive cancellation context
```

On the first dispatch failure, the Scheduler cancels its child context, stops claiming,
waits for active dispatches to return, and reports that failure to the Runtime. Any Cell
left `CLAIMED` is returned to `READY` by normal startup recovery.
## Scheduler non-goals

The Scheduler does not understand:

- workflows;
- dependencies;
- business priorities;
- retry policies;
- handler semantics;
- payload contents.

The Scheduler only understands:

- Cell state;
- execution time (NotBefore).

## Future considerations

The current design intentionally leaves room for:

### Scheduler wake-up

A future Listener may notify the Scheduler:

"New work exists; check the Store."

This avoids unnecessary polling.

### Smarter NotBefore handling

A future implementation may maintain an ordered view of future Cells and sleep until the next eligible execution time.

### Advanced worker scheduling

Future implementations may optimise worker capacity without changing Cell lifecycle semantics.
