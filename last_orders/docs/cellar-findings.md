# Cellar Integration Findings

## Confirmed working in this prototype

1. `Store.Add` + `ClaimNext` + `Complete` lifecycle through public types.
2. `Retry` semantics for transient handler failures.
3. Handler-driven creation of future timed child cells (`Complete.NewCells + NotBefore`).
4. Recovery call pattern (`Recover`) before execution loop starts.

## Interface friction discovered

1. This was previously a blocker, but has now been addressed by Cellar.
2. Public runtime assembly and SQLite constructors are now exposed for downstream modules.

## Consequence for this prototype

1. The prototype now uses public Cellar worker/scheduler constructors directly.
2. The prototype now supports the public SQLite store package for durable restart testing without importing Cellar internal packages.

## Cellar updates now available

1. Public worker constructor is available via `cellar.NewWorker`.
2. Public scheduler constructor is available via `cellar.NewScheduler`.
3. Public SQLite store constructors are available via `cellar/pkg/sqlite` (`NewStore`, `Open`).
4. The updated Cellar agent guide now documents this integration path.
