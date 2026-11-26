# CDD: Winning Venue Statistics and Autopopulate

**Status:** ready-for-agent
**Date:** 2026-11-26

## Problem Statement

The winning-Venue statistics page and the Active Polls autopopulate workflow still read historical Meets directly from Firestore. The administrator's candidate-list diagnostics also reads open Meets directly. Those reads bypass Last Orders and currently mix Meets from different Groups. Autopopulate additionally subscribes to history for every displayed Meet before anyone presses its button, while candidate selection is performed in the frontend.

## Solution

Last Orders calculates winning-Venue statistics on demand for a Group. The statistics page reads those aggregates through the API. A user presses a Meet's autopopulate button to ask Last Orders to select and add candidates, and an administrator can request a read-only preview of the same candidate pools. Diagnostics discovers open Meets through the existing Group-scoped API.

Keep the established selection policy and its existing ranking quirks. Make each candidate addition independent so a failure can leave confirmed partial results. The separate proposal to derive completion from the selected Venue is not part of this work.

## User Stories

1. As an administrator, I want winning-Venue statistics for the selected Group, so that another Group's Meets do not affect the results.
2. As an administrator, I want to choose a historical window, so that I can compare Venues over the period I need.
3. As an administrator, I want the window to include today, so that a completed Meet on today's date is reflected.
4. As an administrator, I want dates interpreted in London time, so that the reporting day is consistent with the application.
5. As an administrator, I want counts and latest winning dates per Venue, so that I can understand selection history.
6. As an administrator, I want known Venues with no wins included in rankings, so that I can identify rarely selected Venues.
7. As an administrator, I want historical winning IDs retained when a Venue is unavailable, so that its recorded wins are not silently lost.
8. As an administrator, I want the current selected Venue counted after a reschedule, so that the report describes the Meet's current outcome.
9. As an administrator, I want Venue-type filtering and ranking limits retained, so that the existing report remains useful.
10. As an administrator, I want a statistics error when the source is unavailable, so that a failure is not presented as an empty report.
11. As an administrator, I want malformed completed history reported as an error, so that incomplete records do not silently distort statistics.
12. As a Group reader, I want statistics access to follow Group visibility, so that private Group history is not disclosed.
13. As a user who can add candidates, I want to press autopopulate for an open Meet, so that the backend adds suitable Venues on demand.
14. As a user who can add candidates, I want no statistics request merely from displaying the button, so that unused actions do not perform unnecessary work.
15. As a user who can add candidates, I want the action to add up to three distinct new Venues, so that existing candidates remain available.
16. As a user who can add candidates, I want banned, recently selected, non-pub and already-listed Venues excluded, so that the suggestions follow the established policy.
17. As a user who can add candidates, I want the existing most-visited, least-visited and general pools used, so that candidate variety is retained.
18. As a user who can add candidates, I want the established expansion and fallback behaviour preserved, so that the migration does not silently change selection policy.
19. As a user who can add candidates, I want an empty result when the Group has no completed history, so that existing no-history behaviour is preserved.
20. As a user who can add candidates, I want a clear no-candidates result, so that I know why no Venue was added.
21. As a user who can add candidates, I want another candidate tried after a definite Venue-specific failure, so that a single unavailable Venue need not prevent useful additions.
22. As a user who can add candidates, I want already-confirmed additions retained if a later addition fails, so that successful work is not lost.
23. As a user who can add candidates, I want an operation stopped on a permission or database failure, so that it does not continue without the required authority or source.
24. As a user who can add candidates, I want an uncertain write outcome reported without trying another Venue, so that an extra Venue is not added accidentally.
25. As a user who can add candidates, I want only additions made by my action counted, so that a concurrent user's addition is not misreported as mine.
26. As a user who can add candidates, I want another deliberate button press to start a new action, so that I can add further candidates when desired.
27. As a user who can add candidates, I want the action to stop if voting closes, so that it does not add candidates to a completed Poll.
28. As a user without candidate-add permission, I want the backend to reject the action, so that the interface cannot bypass authorisation.
29. As an administrator, I want to open a read-only candidate-pool preview, so that I can inspect the policy without changing the Meet.
30. As an administrator, I want diagnostics restricted to the current Group's open Meets, so that I inspect the relevant candidates.
31. As an administrator, I want preview failures distinguished from empty pools, so that unavailable data is not misleading.
32. As an administrator, I want each confirmed candidate addition audited under the requesting user, so that changes remain attributable.
33. As an operator, I want source and authorisation failures surfaced without leaking private details, so that clients can respond appropriately.
34. As a developer, I want the API contract to describe these reads, actions and partial results, so that frontend and backend implementations agree.

## Implementation Decisions

- HandPump owns three API capabilities: a Group-scoped winning-statistics read, a Meet-specific read-only candidate-pool preview, and a Meet-specific autopopulate action. Update the public API contract with their routes, request and response schemas, access rules, error and partial-result behaviour. Keep API representations independent of Firestore storage.
- Calculate historical aggregates fresh on each request using authoritative completed Meet history for the requested Group and inclusive date window. The existing current and open projections are not complete historical archives. Do not create a statistics cache, stream, shared frontend response or internal HTTP dependency between these capabilities. Reuse one backend calculation directly for statistics, preview and action.
- Statistics accept the report's configurable calendar-year window, from that many years before today through today inclusive, using the Europe/London calendar date. Return per-selected-Venue counts and latest selected dates without truncating to top or bottom results. Count only the current selected Venue, not earlier selections from rescheduling. The frontend merges aggregates with the existing Venue catalogue, including zero-win Venues and retaining unknown winning IDs, then performs report filtering and ranking.
- An empty, successfully queried history is distinct from a source failure. A completed Meet in the window without a valid selected Venue is inconsistent and causes an error, not an empty or partial aggregate. A valid selected Venue ID whose Venue record is missing or deleted still contributes its historical win. Preserve the existing stored completion flag for this change; its removal is separately tracked.
- Statistics reads follow existing Group visibility: public Groups allow anonymous access, private Groups require membership or global-admin authority, and invalid supplied credentials are rejected. The preview requires an authenticated global administrator. The action requires authenticated Group access and the same candidate-add permission or global-admin inheritance as a manual addition. Follow existing inaccessible-resource and unavailable-authorisation conventions.
- The action calculates fresh Group history and candidate pools only when requested. Each consumer makes its own request; no result is passed between Meet components. The statistics page requests fresh results on entry and when its window changes, with no background refresh while it remains open. The diagnostics preview is requested on opening the panel and represents that request's observed state, not a promise about a later action.
- Preserve the current one-year historical window for autopopulate, four-week recent-win exclusion inclusive of the cutoff date, pub-only eligibility, banned and existing-candidate exclusions, ranking by count and established tie ordering. Include zero-win Venues in the ranked catalogue. If there are no completed Meets in the window, all pools are empty.
- Preserve the most-visited and least-visited ranking windows beginning at five and expanding by five until three unique viable Venue IDs are exposed or the existing loop terminates. Preserve the current omission of a final partial window and each empty category's fallback to the full viable pool. Preserve the existing treatment of historical unknown Venue IDs and fallback sampling weights, including duplicates in the combined fallback pool.
- Select randomly from most-visited, least-visited and general pools in that order, then use the existing combined-pool backfill to seek up to three distinct additions. Existing candidates do not reduce the target of three additional Venues. If an attempted Venue has a definite Venue-specific failure, try another unattempted eligible Venue from that pool until successful or exhausted, then continue with the established remaining pools and fallback. Never attempt the same Venue twice in one action.
- Each candidate write retains existing Meet-open and Venue existence, deletion and name validation. Do not add a separate eligibility recheck between collecting pools and writing candidates. A concurrent already-present candidate is a no-op, is not counted as this action's addition, and causes another candidate to be tried. Stop if the Meet closes. Preserve one existing candidate-add audit per actual addition, attributed to the authenticated caller; no-op and failed writes do not create successful-add audits.
- Keep confirmed successful additions on later failure. Abort on permission failures, database connection or other systemic failures, and uncertain write outcomes; do not try a replacement after an uncertain outcome. When a response can be delivered, an aborted action returns a structured error containing confirmed additions, the stopping reason and any uncertain outcome. A lost HTTP response may prevent delivery of this information; never claim it was received. Do not automatically retry, roll back confirmed additions, or introduce an idempotency store. A later deliberate button press is a new action.
- Exhausting viable candidates without a systemic failure is a successful result reporting the actual additions, including zero. The button remains available without prefetching and displays a pending state while its action runs. Report empty, partial, aborted and uncertain outcomes distinctly; do not present unavailable data as an empty pool.
- Diagnostics uses the existing Group-scoped open-Meets API instead of a direct Firestore listener and displays loading, stale and error states appropriately. It requests read-only preview pools per Meet and does not mutate them. The action recomputes pools when pressed; preview and action can differ if underlying state changes.
- Follow the existing Last Orders composition, endpoint and service responsibilities. Extend existing write results as necessary to distinguish a committed addition from a no-op; do not infer success solely from the returned Meet state. Update relevant API and workflow documentation. Preserve unrelated direct Firestore access and existing security rules.

## Testing Decisions

- Good tests assert observable behaviour at stable public boundaries, rather than private helper calls or internal write sequences. Prefer existing interfaces and test infrastructure over additional test-only abstractions.
- Exercise all three capabilities through the Last Orders HTTP boundary, following existing Meet-write, historical-statistics and authorisation tests. Cover Group isolation and public/private access, windows and London dates, zero wins, unknown winning IDs, rescheduling, empty history, invalid completed records, source failure and report results.
- Use existing Firestore emulator-backed Meet-write tests to verify actual additions and individual audit records, no-op races, closed Meets, missing or deleted Venues, candidate replacement, partial success and aborted or uncertain outcomes. Verify no failed or no-op addition produces a successful-add audit. Use controlled failures at existing boundaries where an emulator cannot reliably produce them.
- Cover selection through the API with deterministic test data and controlled randomness at an existing suitable boundary. Verify pool membership, four-week cutoff, window expansion, final-partial-window and fallback-weight quirks, deduplication, up to three additional candidates, exhausted pools and the agreed retry behaviour without asserting implementation call order.
- Exercise React workflows at the existing component and hook boundaries with mocked Last Orders responses. Cover on-demand actions without eager statistics requests, pending and repeat-click handling, no candidates, partial and uncertain results, statistics filtering and windows, Group switching, diagnostics preview and open-Meet loading and errors.
- Add focused backend-dependent browser coverage using the existing emulator-based harness if needed to verify the end-to-end action and admin preview. Do not use production Firebase, deploy, mutate production data or tighten live rules for verification.

## Out of Scope

- Removing the stored completion flag, repairing historical data or changing the Meet completion model.
- Fixing ranking-window or fallback-weight quirks, changing selection policy, or enabling autopopulate with no completed history.
- Automatic Poll completion, voting, attendance statistics, unrelated Firestore access and Firestore rules changes.
- Statistics caching, live statistics streams, durable autopopulate jobs, automatic retries, request idempotency and transactional all-or-nothing addition.
- Deployment, production data changes or live index checks.

## Further Notes

The existing direct historical query spans Groups; Group scoping is an intentional change. The existing automatic Poll completion service selects a winning Venue and is distinct from this candidate autopopulate action. A preview is an observation, not a reservation. The three writes in an action are not an atomic batch, and a lost response may leave the caller uncertain about the result.
