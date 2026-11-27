# Autopopulate Contract

Last Orders owns candidate-pool calculation and candidate writes. Rendering an
Active Poll button performs no history or candidate-pool request. A deliberate
press sends one `POST /api/meet/{meet_id}/autopopulate`; each press is a new
request and the backend recalculates from current state.

The backend preserves the established one-year history, four-week inclusive
recent-win cutoff, pub-only eligibility, banned and existing-candidate
exclusions, ranking windows, category fallbacks, and weighted fallback pool.
An action attempts up to three distinct additions even when the Meet already
has candidates. No completed history produces empty candidate pools.

Each addition is written and audited independently. Concurrent candidates that
are already present do not count as additions. A definite Venue-specific
failure allows another eligible Venue to be tried. Permission or systemic
failures stop the action; uncertain writes also stop it to avoid accidental
extra additions. Confirmed additions are retained and returned with an abort
result when the response is delivered. The action has no automatic retry or
rollback.

The action response distinguishes `complete`, `exhausted`, and `aborted`
outcomes and reports confirmed additions. A lost response can leave the caller
uncertain about the result. The Active Polls button remains available after an
empty or exhausted result and prevents overlapping submissions while pending.

Administrators can inspect a read-only observation through
`GET /api/meet/{meet_id}/candidate-pool-preview`. It does not reserve or add
candidates, and its result may differ from a later action. Diagnostics loads
previews only when the candidate-list panel is opened and obtains its open Meets
from the Group-scoped Last Orders API.

The public schemas, access rules and response behavior are documented in
[`Last Orders API contract`](../../last_orders/docs/pubnightpicker-openapi-v1.yaml).
