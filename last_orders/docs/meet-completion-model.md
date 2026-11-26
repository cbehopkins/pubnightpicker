# Meet Completion Model

A Meet is complete when its `selected` Venue ID is a non-empty string. A missing
or empty `selected` value is incomplete. The unexpected value `"any"` counts as
complete and must be logged as an error; existing Venue lookup behavior handles
the invalid ID.

The Go API keeps `voting_complete` as a derived response field for clients that
use it. During the compatibility window, writers continue to maintain the
stored `completed` field alongside `selected` so older clients keep working.
The final feature ticket removes those writes after the maintainer's chosen
window.

## Read-only audit before rollout

Audit the intended Firestore project without writing data. Report counts for:

- Meets with a missing, null, or empty `selected` value.
- Meets with a non-empty `selected` value and `completed` missing or false.
- Meets with no selected Venue and `completed` true.
- Meets where `selected` is `"any"`, is not a string, or references no Venue.

Firestore equality queries for `selected == ""` do not include documents where
the field is missing. Before deploying readers that use that query, ensure old
open Meets are represented with an explicit empty string. Reconcile any
`completed` mismatch from the agreed selection invariant so old clients see the
same status during dual-write. Do not infer a Venue for records without one.
Production data changes require explicit authorization and must be reviewed
against the audit report first.

Past-Meets pagination stays date-ordered by scanning and filtering Meet
documents until it has a full page of selected Meets. This reads more documents
than the old completion-field query when a Group has many open historical
Meets; review that cost if those collections grow substantially.

## Rollout

Drain older writer versions before the read-only audit so they cannot create new
records without `selected`. Review the audit and apply any separately authorized
data repair before deploying the selection-based readers and queries. The new
writers set `selected` (empty for open Meets and the Venue ID for completed
Meets) and keep the legacy `completed` dual-write. Older clients can therefore
continue reading `completed` during the compatibility window. The maintainer
chooses when that window has elapsed; then complete the final feature ticket to
stop writing the field.
