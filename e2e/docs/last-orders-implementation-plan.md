# Last Orders Browser Journey Implementation Plan

Status: phases 1-2 implemented. The default browser runner executes frontend-only
and Last Orders groups sequentially in separate fresh emulator lifetimes. Two
backend recurrence scenarios cover calculation/editing and due-poll
materialisation. Phases 3-9 below remain planned, including their explicit
readiness/correlation decision gates. The Python notifier worker and its legacy
E2E scripts are retired; Python is used for bootstrap and orchestration only.

Local verification: both recurrence scenarios passed independently; the default
command passed six frontend and two backend scenarios. Focused harness and
calendar/wait checks accompany the runner. A dedicated CI workflow runs both
groups and retains synthetic failure artifacts for seven days; its hosted run
is not implied by local verification.

## Goals

- Include backend-dependent journeys in the standard browser E2E suite.
- Keep journeys which do not need Last Orders free of backend side effects.
- Exercise the real React application, Firebase emulators and Last Orders work
  processing, substituting only external email and browser push delivery.
- Verify recurrence first, then voluntary push opt-in, poll-open and
  completion/reschedule notifications, test email/history, and diagnostics.
- Keep auto-completion out of scope until a deterministic clock-control design
  is agreed separately.
- Prevent access to production Firebase, real email providers and real push
  services.

## Suite Structure

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
invoke this browser runner without launching the retired Python notifier
worker.

The current Cucumber/Playwright tests are Gherkin features, not test classes.
Mark backend-dependent features with `@last_orders`; run frontend-only features
with `not @last_orders`. Keep scenario steps and browser contexts in the existing
Cucumber framework rather than introducing a second browser-test framework.

Run the groups sequentially in separate emulator lifetimes. Start Last Orders
once for its group, not once per scenario. Use a fresh temporary SQLite database
for that group; never reuse a developer or deployment database. Individual
scenarios must remain independently runnable, use unique identities and scope
assertions to their own records. Do not assume empty collections after another
scenario has run. The backend must not run while shared emulator data is reset.

The frontend-only completion journey expects the notification-handshake
warning because no application backend runs in that group. Preserve that
assertion there. Backend-dependent completion checks must instead wait for and
assert the Last Orders acknowledgement and correlated work outcome.

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
7. Start Vite and run the selected Cucumber tag group.
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

### Cucumber Group Selection And Legacy Harness Retirement

- Add focused frontend-only and `@last_orders` commands. Run Cucumber with
  `not @last_orders` for frontend-only features and `@last_orders` for backend
  features, using the existing feature glob and step definitions.
- Make `npm run test:journey` execute both groups sequentially by default. Keep
  `run-tests.ps1 -E2E` pointed at this orchestrator; Go/backend startup failure
  must fail the command rather than skip the tagged group.
- Remove `e2e/run_e2e_tests.py` and `e2e/test_e2e_chat_push.py` from supported
  E2E execution. Remove the worker-specific harness and standalone
  `frontend-client.js` test script; keep only Python utilities used for
  bootstrap, debug or orchestration.
- Python may be used for emulator bootstrap, debug utilities and this
  orchestration only. Do not start the Python notifier worker as a test backend.
- Run each group in a separate fresh emulator lifetime. Never run Last Orders
  while resetting emulator state or reuse frontend-group Firestore data.

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

## Phase 6: Completion And Reschedule Notifications

Add backend-tagged browser scenarios which complete and reschedule polls through
the React UI. Reuse the frontend poll-lifecycle steps where practical, but keep
these scenarios in `@last_orders` so they expect Last Orders processing rather
than the frontend-only missing-backend warning.

- Complete a poll and verify the selected venue and poll state in the UI.
- Await the correlated dummy mailing-list email, personal completion email and
  eligible push notification. Exercise personal opt-in and opt-out separately
  from the mailing-list send; include at least one recipient using the login
  email and one using an override address.
- Reschedule a current event through the UI. Verify its new date in Current
  Events and a correlated `poll_rescheduled` push submission. Assert that the
  reschedule is not misclassified as a normal poll completion.
- For all negative recipient assertions, wait for a positive processing barrier
  before asserting that no dummy send exists. Match poll ID and recipient rather
  than searching the whole log for an email address.

Acceptance: completion and reschedule produce only their intended dummy sends,
with recipient selection matching the saved preferences. These assertions
prove backend submission, not real provider or device delivery.

## Phase 7: Test Email And Email History

Add a Preferences journey for a signed-in user to request a test email. Match
the backend acknowledgement to the generated request ID and verify one dummy
email send to the effective destination (login email by default or the saved
override). Keep provider delivery mocked.

After creating a known send, open email history through the normal UI and verify
the owner can see its status and timestamp. Use a second account to verify that
the first account's history is not exposed. Keep the assertion within the
history window the interface promises; do not require old records outside it.

Acceptance: the UI request, acknowledgement, dummy send and history view are
correlated to one user/request, and a different user cannot read that history.

## Phase 8: Diagnostics

From the Diagnostics page, verify that the Last Orders ping endpoint responds
and reports the Firestore emulator check as healthy. Assert the displayed
health/result state and a structured, non-sensitive backend record where useful;
do not assert against free-form log text. A failure-state scenario is optional
and must use an isolated, controllable failure rather than destabilising the
shared emulators.

Acceptance: a healthy backend and its emulator database are visible through the
real diagnostic UI, with no request leaving the local demo environment.

## Phase 9: Admin User Deletion

Keep deletion out of the default backend browser group until the harness has a
separate, explicit deletion configuration. Begin with a dry-run journey:

- An admin deletes a synthetic target through Manage Users.
- Verify the UI summary and app-data scrub: votes and attendance are removed,
  messages are anonymised, push endpoints and role entries are removed, and
  private/public profile documents are deleted.
- Verify the queued request reaches `dry_run_validated` and its audit evidence
  is recorded. In dry-run mode the Firebase Auth account remains; do not assert
  that the target can no longer sign in.

Only add real Auth deletion as a separate opt-in emulator acceptance command.
It must require explicit Auth-emulator routing and deletion flags, use a unique
synthetic account, and verify that account is removed while another account
remains usable. The standard E2E command must never enable this mode or target a
live Firebase project.

Acceptance: default deletion coverage proves safe data scrubbing and dry-run
validation only. Auth deletion is tested solely in a separately guarded demo
Auth emulator run.

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
- Automatic completion and its timer/date-boundary tests remain deferred until
  a separate deterministic clock-control design is agreed. Existing internal
  `ObservedOn` values are not, by themselves, a Playwright clock-control plan.
- Destructive housekeeping.
- Generic daily poll creation until its product contract and Go implementation
  are defined.
- Notification retries, failure injection and restart/replay delivery guarantees.
- Split HandPump/Landlord deployment coverage; start with the combined binary.
