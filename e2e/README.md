# E2E Browser Journeys

End-to-end browser journeys for PubNightPicker use Cucumber.js and Playwright.
The supported E2E path runs the React application against fresh Auth and
Firestore emulators. Backend-tagged scenarios also run the combined Last Orders
binary with dummy email and push delivery. The retired Python notifier worker
is never started.

## Setup

### Prerequisites

- Node.js 20+ with npm (required by the Cucumber/Playwright browser journey)
- Python 3.12+ with Poetry (used by the emulator bootstrap utility)
- Firebase CLI (`npm install -g firebase-tools`)
- Java 21+ for the Firebase emulators
- Go as specified by `last_orders/go.mod`, with `GOTOOLCHAIN=auto` if needed
  (not required for the focused frontend-only command)
- The browser journey starts emulators on ports 8080 and 9099, plus Firebase hub
  port 4400, logging port 4500 and Firestore websocket port 9150. Vite uses 3000
  and Last Orders uses 8081. The selected group's ports must be free.
- For the browser journey: the React dependencies installed. Installing the E2E
  npm dependencies also installs the matching Playwright Chromium browser.

### Installation

```bash
# Install Node dependencies
cd react
npm ci
cd ..
cd e2e
npm ci
cd ..

# Install Python dependencies (via poetry in firebase_sub)
cd firebase_sub
poetry install
cd ..
```

## Running E2E Tests

### Via Consolidated Test Runner

```powershell
# From repository root (PowerShell)
./run-tests.ps1 -E2E              # E2E tests only
./run-tests.ps1 -All              # All test suites (Unit + Integration + E2E)
./run-tests.ps1 -Integration      # Integration tests only (default)
./run-tests.ps1 -Unit             # Unit tests only
```

### Browser journeys

The [Last Orders implementation plan](docs/last-orders-implementation-plan.md)
tracks the implemented orchestration, recurrence and voluntary push opt-in
milestones, and the later delivery-evidence, notification, email-history,
diagnostics and deletion phases.

From `e2e/`, run `npm ci` to install dependencies and Chromium, then run:

```bash
npm run test:journey
```

The default command (also `npm test` and root `./run-tests.ps1 -E2E`) runs both
mandatory groups sequentially:

| Group | Cucumber selection | Infrastructure |
| --- | --- | --- |
| Frontend-only | `not @last_orders` | Fresh emulators and Vite; no backend |
| Backend-dependent | `@last_orders` | Different fresh emulators, Vite and Last Orders |

Both groups must discover scenarios. A build, startup, scenario or cleanup
failure fails the command rather than skipping the backend group. Last Orders
is built before services start and runs once for its group, using a temporary
SQLite database. No emulator data is imported, exported or reset between
scenarios. The initial poll-history floor is UTC today; expected recurrence
dates use Europe/London. A London midnight crossing explicitly fails the
recurrence scenario so it can be rerun with fresh data.

Focused developer commands, from `e2e/`:

```bash
npm run test:journey:frontend
npm run test:journey:last-orders
npm run test:journey:last-orders -- --name "monthly Event"
npm run test:harness
npm run test:support
```

The harness checks use standard-library Python `unittest`; support checks use
Node's built-in test runner. Discovery runs before services start. CI runs both
checks and the default browser command, not just one tag group.

The `postinstall` hook installs the browser required by the local Playwright
version. If npm lifecycle scripts were disabled or the browser cache was removed,
run `npm run install:browsers` from `e2e/`. The root dependency updater runs this
step explicitly after E2E npm updates, even though it disables other lifecycle
scripts.

The journey uses a fresh, empty emulator database (no import) with project ID
`demo-pubnightpicker-journey`. It creates `journey-admin@test.local` with the
bootstrap CLI and an E2E-only password, opens the homepage, signs in, and checks
that the administrator can reach My Preferences. Firestore and Auth use the
ports already configured by the React app (8080 and 9099). The runner refuses
occupied ports rather than reusing or terminating another service. On Windows,
each owned process tree is enclosed in a kill-on-close Job Object; POSIX uses
process groups. Cleanup stops and waits for backend/frontend children before
their emulators, including on failure or interruption. It never kills a
process merely because its name or port resembles an emulator.

Children receive allowlisted OS/tool settings and explicit demo configuration,
not inherited credentials or provider/deployment settings. An E2E Vite wrapper
also disables developer env-file loading. The backend uses explicit loopback
Firestore/Auth endpoints and Auth-emulator permission, a generated cursor key,
dummy push, and dummy email verified from structured startup evidence. Admin
deletion stays disabled and dry-run stays enabled. No backend VAPID private key
or real-delivery configuration is supplied.

The runner discovers all `features/**/*.feature` files. The chat-permissions
journey registers Alice through the UI, checks that she has no Chat Page link,
and signs the bootstrapped admin into a separate Playwright browser context.
The admin finds Alice in Manage Users and grants Can Chat on her user-detail
page. Alice's existing session must then show the Chat Page link and open chat,
without a reload or another login. Each scenario gets a new browser, and named
actors have isolated browser contexts. Alice uses a unique email address so
the journey does not depend on other scenarios' accounts; emulator data is
fresh per run, not reset between scenarios.

The poll-lifecycle journey keeps Bob, Claire and the admin in separate browser
contexts. Bob and Claire register through the UI, and the admin grants both
Can Add/Delete Poll Pubs, Can Create/Delete Poll and Can Chat; only Claire
receives Can Complete/Reschedule. Bob creates tomorrow's poll, filters the venue picker
to Pub and adds two pubs. Claire sees the poll and pubs arrive live and votes
for both; Bob sees those totals and adds his vote. Claire chooses the winning
pub, both users see the poll disappear from Active Polls, and both find the
meet in Current Events. They then exchange messages in global and event chat,
verify that messages remain in their intended scope, and reopen event chat to
check persistence. Vote totals are checked without granting Can Show Voters or
administrator access to either member.

Only the venue catalogue is seeded directly: two pubs and a restaurant, using
the Firebase Web SDK authenticated as the bootstrapped admin against the
emulators. Unique venue names and account emails keep the fixture separate
from other scenarios. The restaurant verifies that the Pub filter excludes
other venue types. These fixture pubs serve food, so completion needs no
separate restaurant plan. Since Last Orders does not run in the frontend-only
group, the journey accepts the application's explicit notification-handshake
warning when completing the poll; it does not test email or push delivery.

The venue-management outline runs independently for Pub, Restaurant and Event.
Two members register through the UI and receive Can Manage Venues from the
admin. One creates a venue; the other edits its name, address, website, map,
notes and attributes. The creator sees the renamed venue without reloading,
and both members reopen the saved details. The second member also corrects
the venue type (Pub to Restaurant, Restaurant to Event, or Event to Pub),
preserving the venue's identity. Type filters and the poll picker must reflect
the correction. Restaurant food availability is implicit rather than an
editable checkbox. The creator also receives poll creation and venue-selection
permissions to check the picker using a temporary poll, which is deleted
afterwards. Venue data is created entirely through the UI with unique names.
This outline remains frontend-only. The separate `@last_orders` recurrence
feature registers its own actors and creates Events entirely through the UI.
It verifies calculation and another member's edit of a monthly occurrence
beyond the poll-creation window, then independently verifies a weekly Event
due tomorrow materialises a poll, votes and attendance. Read assertions use the
authenticated emulator SDK and correlate by venue ID and occurrence date;
computed results are never seeded. These scenarios do not prove restart/replay
idempotency or notification delivery.

The frontend-only push feature covers allowed permission and saved categories,
denied permission, and disabling push, with fresh accounts in each scenario.
It checks that no permission request or subscription occurs before clicking
Enable Push, and verifies the app's persisted preference and endpoint documents
after enabling, saving, revisiting and disabling.

Every browser context installs a test-only permission/PushManager fixture before
application scripts. Unconfigured actors default to denying permission. The
fixture preserves real local service-worker registration and all application
Firestore writes, but supplies observable synthetic subscriptions instead of
contacting browser push services. Attempts to contact the synthetic delivery
endpoint are blocked and fail the push assertions.

The runner reuses one generated **public-only test VAPID key**; it does not
generate a new VAPID pair per scenario or keep a VAPID private key. Per-actor
`p256dh` and `auth` values are synthetic subscription encryption keys, not VAPID
credentials. They distinguish browser endpoints and exercise serialisation.
These tests do not establish native permission-dialog behaviour, VAPID signing,
provider delivery or OS notification display. Production VAPID key rotation is
not part of this fixture.

## Debugging

The runner reports progress and the per-run `e2e/artifacts/<run>/<group>/`
directory. It contains discovery and Cucumber JSON reports, failure screenshots
embedded in the latter, and backend stdout JSON Lines plus separate stderr for
the backend group. Backend failure output includes bounded log tails. Artifacts
are ignored by Git; CI retains synthetic failure artifacts for seven days.

The E2E emulator UI is disabled. Local logs and artifacts contain synthetic
journey users and generated subscriptions only; do not introduce real identities
or provider credentials into fixtures.

## Troubleshooting

### Emulator fails to start
- Check whether ports 8080, 9099, 4400, 4500, 9150, 3000 and (for backend) 8081
  are occupied.
- Stop only the emulator process that you started; the runner refuses occupied
  ports and cleans up its own emulator process group on exit.

### Poetry bootstrap fails
- Ensure Poetry is installed and the `firebase_sub` environment is installed.

### Last Orders fails to start
- Check the reported backend stderr artifact and Go prerequisites. Backend
  startup failures are errors, never successful skips.
- The runner supplies its own temporary database, poll-history floor and cursor
  key. Do not work around failures with production env files or credentials.
