# Firestore Schema Registry

This directory is the canonical, machine-readable Firestore contract for the current production system.

Scope:
- Included: active production contract consumed by React and firebase_sub.
- Excluded: rewrite-only assumptions in cellar, last_orders, and sweego_client until promoted.

Why this exists:
- Firestore has no built-in central schema registry.
- Rules, app code, and backend code can drift.
- This directory provides one source of truth for document shapes and collection intent.

## Structure

- contract-manifest.json: collection inventory, ownership, and status.
- collections/*.schema.json: JSON Schema documents for collection payloads.

## Governance

- Status values:
  - authoritative: active, production-supported contract.
  - candidate: proposed contract not yet promoted.
  - deprecated: legacy contract retained for compatibility windows.
- Every collection schema should declare x-schemaVersion.
- Breaking changes require a version bump and migration notes in commit/PR text.

## Current source inputs used to seed this registry

- react/docs/firestore-data-contract.md
- react/firestore.rules
- firebase_sub/firestore.rules

## Next steps

1. Generate TypeScript validators/types from these schemas for react.
2. Validate firebase_sub writes against these schemas in tests.
3. Add CI drift checks so schema updates are explicit.
