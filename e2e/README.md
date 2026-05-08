# E2E Test Suite

End-to-end tests for PubNightPicker chat push notification flow.

## Architecture

The E2E test harness uses a mixed approach:

1. **Node Frontend Client** (`frontend-client.js`) — Firebase Web SDK connects to Auth emulator, authenticates, and posts a chat message to Firestore
2. **Python Orchestrator** (`test_e2e_chat_push.py`) — Coordinates the test flow:
   - Seeds smoke users via firebase-admin
   - Spawns Node frontend client
   - Spawns Python notifier subprocess in dummy mode
   - Waits for notifier to process the message
   - Asserts on either Firestore side effects (`chat_push_actions`) or notifier logs
3. **E2E Runner** (`run_e2e_tests.py`) — Manages emulator lifecycle and test execution

## Setup

### Prerequisites

- Node.js 18+ with npm
- Python 3.12+ with Poetry
- Firebase CLI (`npm install -g firebase-tools`)
- Emulators running on ports 8180 (Firestore) and 9199 (Auth)

### Installation

```bash
# Install Node dependencies
cd e2e
npm install
cd ..

# Install Python dependencies (via poetry in firebase_sub)
cd firebase_sub
poetry install
cd ..
```

## Running E2E Tests

### Direct E2E Tests Only

```bash
# From repository root
poetry run python e2e/run_e2e_tests.py
```

### Via Consolidated Test Runner

```powershell
# From repository root (PowerShell)
./run-tests.ps1 -E2E              # E2E tests only
./run-tests.ps1 -All              # All test suites (Unit + Integration + E2E)
./run-tests.ps1 -Integration      # Integration tests only (default)
./run-tests.ps1 -Unit             # Unit tests only
```

## Test Flow

1. **Initialize Firebase**: Connect to emulators (Firestore on 8180, Auth on 9199)
2. **Seed Smoke Users**: Create deterministic test users (smoke-admin, smoke-user-a, smoke-user-b) with push endpoints
3. **Frontend Client**: Authenticate as smoke-user-b and post a global chat message
4. **Notifier Subprocess**: Start Python notifier in `--dummy-push` mode (logs instead of sending actual web pushes)
5. **Wait for Processing**: Poll for either:
   - `chat_push_actions/{messageId}` document written to Firestore (side effect)
   - Or "Chat push delivery:" log message from notifier (evidence of processing)
6. **Assert Success**: Exit with status 0 if evidence found, 1 otherwise

## Test Data

### Smoke Users (Seeded by `seed_smoke_data()`)

| UID | Email | Role | Push Enabled | Global Chat | Event Chat |
|-----|-------|------|--------------|-------------|-----------|
| `smoke-admin` | smoke-admin@test.local | admin (all roles) | N/A | N/A | N/A |
| `smoke-user-a` | smoke-user-a@test.local | none | false | N/A | N/A |
| `smoke-user-b` | smoke-user-b@test.local | none | **true** | **true** | **true** |

Smoke-user-b has a deterministic push endpoint at `users/smoke-user-b/push_endpoints/smoke-endpoint-b`.

## Notifier Log Patterns

The E2E test assertions key off these log messages:

```
Chat push delivery: delivered=1 retryable_failures=0
```

## Debugging

### Enable Verbose Logging

```bash
cd e2e
poetry run python test_e2e_chat_push.py
```

Output includes:
- Notifier subprocess logs (prefixed with `[Notifier]`)
- Orchestrator progress (prefixed with `[Orchestrator]`)
- Firestore side effect checks

### Manual Emulator Start

```bash
cd firebase_sub
firebase emulators:exec --project demo-firebase-sub-integration --only firestore,auth
```

Then in another terminal:
```bash
cd e2e
node frontend-client.js "Test message"
```

### Inspect Firestore Emulator Data

Navigate to http://localhost:4000 (Firebase Emulator UI, if enabled).

Or use firebase-admin to query directly:

```python
import os
from firebase_admin import credentials, firestore, initialize_app
import google.oauth2.credentials

os.environ["FIRESTORE_EMULATOR_HOST"] = "127.0.0.1:8180"

app = initialize_app(
    credential=google.oauth2.credentials.Credentials(token="owner"),
    options={"projectId": "demo-firebase-sub-integration"}
)

db = firestore.client()
docs = db.collection("messages").stream()
for doc in docs:
    print(doc.id, doc.to_dict())
```

## Integration with CI/CD

The consolidated runner (`run-tests.ps1`) makes it easy to integrate all test suites:

```yaml
# Example GitHub Actions workflow (Azure DevOps)
- name: Run All Tests
  run: ./run-tests.ps1 -All
```

## Troubleshooting

### Emulator fails to start
- Check if ports 8180 and 9199 are already in use
- Try `firebase emulators:stop` then restart
- On Windows, may need to force-kill lingering Java/Node processes

### Frontend client: "User not found"
- Verify smoke users were seeded: check `users/{uid}` in Firestore
- Ensure Auth emulator is running on port 9199

### Notifier subprocess doesn't process message
- Check if `messages/{messageId}` document exists in Firestore (frontend should have written it)
- Verify notifier logs appear (even dummy-mode errors would show in output)
- Extend `CHAT_PUSH_TIMEOUT_SEC` in `test_e2e_chat_push.py` if system is slow

### poetry commands fail
- Ensure Poetry is installed: `pip install poetry`
- From `firebase_sub` directory, run `poetry install`
