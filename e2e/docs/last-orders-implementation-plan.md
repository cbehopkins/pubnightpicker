# Last Orders Browser Journey Implementation Plan

Status: proposed implementation plan. This document does not enable the backend
or change the current test runner.

## Goals

- Include backend-dependent journeys in the standard browser E2E suite.
- Keep journeys which do not need Last Orders free of backend side effects.
- Exercise the real React application, Firebase emulators and Last Orders work
  processing, substituting only external email and browser push delivery.
- Verify recurrence first, followed by voluntary push opt-in and poll-opening
  notifications.
- Prevent access to production Firebase, real email providers and real push
  services.

## Agreed Suite Structure

`npm run test:journey` will run both groups automatically:

| Group | Selection | Infrastructure |
| --- | --- | --- |
| Frontend-only | `not @last_orders` | Fresh Auth/Firestore emulators and Vite |
| Backend-dependent | `@last_orders` | Fresh Auth/Firestore emulators, Vite and Last Orders |

The tag declares an infrastructure requirement, not an optional test. A failure
in either group fails the standard command. Missing Go tools, failed backend
startup or missing backend dependencies must fail rather than skip the group.
Focused developer commands may select one group, but the default and CI must
include both. Update the documented root E2E entry point if it does not already
invoke this browser runner; keep the existing Python notifier flow separate.

Run the groups sequentially in separate emulator lifetimes. Start Last Orders
once for its group, not once per scenario. Use a fresh temporary SQLite database
for that group; never reuse a developer or deployment database. Individual
scenarios must remain independently runnable, use unique identities and scope
assertions to their own records. Do not assume empty collections after another
scenario has run. The backend must not run while shared emulator data is reset.

The frontend-only completion journey currently expects the missing-notifier
warning. Preserve that assertion there. Backend-dependent completion checks
must instead expect the real handshake acknowledgement, if added later.

## Phase 1: Safe Backend Orchestration

Primary implementation locations:

- `e2e/run_gherkin_e2e.py`: group orchestration and process lifecycle.
- `e2e/package.json`: standard and focused developer commands.
- `e2e/features/support/`: backend availability and artifact helpers as needed.
- `e2e/.gitignore`: generated artifacts if not already covered.
- `e2e/README.md` and the existing CI/root runners where applicable.

### Startup And Shutdown

1. Validate prerequisites and build the Last Orders binary before starting
   services. Run the built binary, not a long-lived `go run` wrapper.
2. Reserve the group's emulator, frontend and backend ports. Fail if occupied;
   do not reuse or terminate unrelated services. Bind HTTP to loopback, using
   port 8081 by default rather than Firestore's port 8080.
3. Start fresh Auth and Firestore emulators without importing or exporting data.
4. Bootstrap the admin using the existing emulator-only bootstrap flow.
5. Start the combined Last Orders composition with the temporary SQLite path,
   explicit dummy push client and Auth emulator permission. Supply a generated
   test-only cursor-signing key and other required runtime configuration.
6. Await `/healthz` with a bounded timeout while monitoring process exit.
   Readiness confirms initial projections and listeners have started, not that
   later work has finished.
7. Start Vite and run the backend-tagged Cucumber scenarios.
8. On success, failure, timeout or interruption, stop and wait for every owned
   process, including Windows child processes. Stop the backend before its
   emulators. Release temporary files after processes close them.

### Safety Contract

- Construct a controlled child-process environment rather than copying all
  shell variables. Retain only required OS/tool settings and explicit test
  configuration. Apply the same discipline to bootstrap and frontend children.
- Require the exact demo project ID and expected loopback Auth/Firestore
  emulator endpoints. Set both Firestore and Auth project IDs explicitly.
- Do not pass `LAST_ORDERS_FIREBASE_CREDENTIALS_JSON`,
  `GOOGLE_APPLICATION_CREDENTIALS`, email-provider settings, VAPID private keys,
  or inherited deployment configuration to the backend.
- Pin `-push-client=dummy`. Email currently selects dummy only when provider
  settings are absent; remove the full Mailtrap and Sweego configuration,
  including provider/base-URL settings, rather than only their main tokens.
- Keep admin Auth deletion disabled and dry-run enabled. Never pass
  `-enable-real-auth-delete` in these phases.
- Do not rely on absent credentials alone: emulator routing must be explicit
  and validated, including protection against Application Default Credentials
  fallback. Reject unsafe configuration before launching the backend.
- Dummy delivery does not disable recurrence, housekeeping or other database
  writes. Only fresh demo emulator data and temporary SQLite state may be used.
- Verify effective dummy-client and emulator selection through startup evidence.
  Where an existing startup record is insufficient, add a small non-sensitive
  configuration record or validation check. Never print environment contents.
- Add focused harness checks for unsafe endpoints/project IDs, inherited
  provider settings, backend startup failure and cleanup failure. These checks
  must not contact external providers or production Firebase.

Acceptance: the standard command runs both groups, the frontend-only group never
starts Last Orders, backend failure cannot be reported as a successful skip,
and neither success nor failure leaves owned services running.

## Phase 2: Recurrence Journeys

Add a backend-tagged recurrence feature and reuse the real registration,
permission-grant and venue form steps. Do not seed the backend-generated
`next_occurrence_date` field or invoke recurrence service methods directly.

### Calculate And Edit The Next Occurrence

- An authorised member creates an Event with a monthly recurrence whose next
  occurrence is safely outside the seven-day poll-creation window.
- Await the persisted `next_occurrence_date` and verify the corresponding
  Next Event display in the venue form.
- Another authorised member changes the recurrence to a different known date.
- Await the recalculated field and verify the saved UI display reflects it.
- Match the venue by identity, not only its name. Do not require deletion or
  rescheduling of an already-created poll as a consequence of a rule edit;
  that behaviour is not part of this test contract.

### Materialise A Due Occurrence

- Create an Event with a weekly recurrence whose next occurrence is tomorrow.
- Await the calculated occurrence and a poll associated with that venue/date.
- Verify the poll appears in Active Polls with the event venue included.
- Verify the associated votes and attendance documents exist.
- Correlate by `eventVenueId` and occurrence date rather than a global poll
  count or a date alone.

Calculate expected dates using Europe/London calendar dates, including month
and year boundaries. Use changes that trigger normal listeners rather than
waiting for daily timers. Handle a London midnight crossing explicitly instead
of accepting arbitrary dates. Extend calendar-rule coverage in focused backend
tests; these journeys establish the UI-to-backend contract.

Acceptance: both scenarios pass through the running backend with bounded
condition-based waits and no direct writes of computed results. Later add a
separate replay/restart scenario proving one poll per venue/occurrence; observing
one poll once is not proof of replay idempotency.

## Phase 3: Voluntary Push Opt-In

Create an independent frontend-only Preferences journey and reusable opt-in
steps for notification journeys. Opt-in itself does not require Last Orders.

- A new user initially has push disabled and no active push endpoint.
- Loading the application and visiting Preferences must not request browser
  notification permission or subscribe automatically.
- Clicking Enable Push requests permission. Granting it creates an endpoint
  through the application's normal Firestore writes and enables the preference.
- Select and save the desired notification categories through Preferences.
- Verify saved preferences and endpoint state after revisiting Preferences.
- Add independent denied-permission and disable-push cases, checking that denial
  does not create an active endpoint and disabling removes eligibility.

Use a Playwright-only browser fixture for the external permission/subscription
boundary. Provide observable request/subscribe calls, controllable permission
responses and a unique synthetic subscription per actor. Preserve real
application opt-in logic and Firestore writes; do not seed `webPushEnabled=true`
or pre-create endpoints to bypass the journey. Use only a generated test public
VAPID key if required by the frontend.

Verify browser capability detection, service-worker registration and subscription
serialisation against the existing implementation before choosing the narrowest
substitution. Keep substitutes in test code; do not add a production bypass or
fall back to real push-service registration. These tests do not establish native
permission-dialog behaviour, push-provider delivery or OS notification display.

Acceptance: no permission request occurs before a voluntary click, allowed and
denied paths are observable, and no external push service is contacted.

## Phase 4: Structured Delivery Evidence

Last Orders already uses a JSON `slog` handler on stdout. Its dummy email adapter
records `msg`, `dry_run`, `recipient`, `message_id` and rendered `message`. The
dummy push sender records endpoint, topic, TTL and message payload. Start with
this output; a new output transport is not required.

- Capture backend stdout as a per-run JSON Lines artifact and stderr separately.
  Keep consuming output while the backend runs so pipes cannot block it.
- Parse JSON records using a structured parser. Surface malformed backend log
  records rather than silently treating them as absent delivery evidence.
- Provide bounded waits that match a specific scenario's recipient/endpoint,
  notification kind and originating poll. Ignore unrelated scenarios' traffic.
- Evaluate whether existing message identifiers and durable records provide
  sufficient correlation. If not, add narrowly scoped structured fields at the
  appropriate backend boundary: notification kind, poll ID and user identity
  where actually available. Do not infer identity from a display name.
- Preserve existing delivery semantics and distinguish non-suppressed dummy
  sends from suppression/dry-run handling.
- Keep full content logging restricted to dummy/test delivery. Generated
  artifacts must contain synthetic test data only, never provider secrets or
  real subscriptions. Ignore local artifacts in Git and retain useful failure
  artifacts in CI under an explicit retention policy.

Acceptance: tests can identify a dummy notification of type X for user Y and
check relevant content Z without matching an entire email or depending on
free-form log formatting. Correlation gaps are resolved explicitly before
claiming recipient-specific coverage; no separate test HTTP API is assumed.

## Phase 5: Poll-Opening Notifications

Add backend-tagged scenarios with independently registered, uniquely named
users and separate browser contexts.

1. Configure email opt-in/address and push opt-in/categories through Preferences.
   Reuse Phase 3 steps to create the synthetic subscription through the UI.
2. Include an opted-out member and known expectations for all test recipients.
   Explicitly set notification suppression off in the isolated test environment.
3. Wait until the backend has observed the relevant recipient and endpoint
   preferences before creating the poll. Use an existing observable projection
   signal if available; otherwise agree a narrow readiness mechanism. Do not
   substitute a fixed sleep or assume an acknowledged browser write means the
   backend projection is current.
4. Create the meet poll through the UI.
5. Await non-suppressed dummy email and push records correlated with that poll
   and the opted-in recipients. Check relevant date, destination link and payload
   fields rather than the complete rendered message.
6. Verify opted-out users are excluded. Use a completed recipient-selection or
   delivery-processing barrier before asserting absence; immediate log absence
   or an arbitrary quiet interval does not prove exclusion.
7. Add a separate case disabling push before a later poll and proving that
   endpoint is not selected. Keep prior delivery evidence distinguishable.

Acknowledgements, `open_actions`/`comp_actions` records and Accepted delivery
states are supporting evidence, not sufficient alone: processing can finish
without recipients or with notifications suppressed. These tests prove selection
and submission to dummy transports, not actual inbox or browser delivery.

## Validation And Rollout

Implement phases in order, preserving a working default suite at each stage.
Run narrow orchestration/parser checks and Cucumber discovery checks before
browser execution. Verify the default command actually executes both groups
once backend scenarios exist. Run the existing journeys unchanged in their
frontend-only environment alongside each new group of scenarios.

Do not silently weaken assertions or add production test bypasses when a needed
correlation/readiness signal is missing. Identify the gap and agree the smallest
supported change before extending the relevant phase.

## Deferred Work

- Real provider delivery, native push prompts and OS notification display.
- Automatic completion, timer advancement and date-boundary scheduling tests.
- Destructive housekeeping and Auth account deletion journeys.
- Notification retries, failure injection and restart/replay delivery guarantees.
- Split HandPump/Landlord deployment coverage; start with the combined binary.
