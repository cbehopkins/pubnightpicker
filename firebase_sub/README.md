# Automated Recurring Event Polls

The `maintain_event_recurrence_polls` housekeeping task (run by the backend worker) automatically creates and completes polls for venues with recurrence rules.

- When a venue of type "event" has a recurrence set, the job:
	- Calculates the next occurrence date.
	- Creates a poll for that date when within the lead window.
	- Marks polls as completed after the event date.
	- Advances the next occurrence date or clears it if no further recurrences.
- This job runs as part of the regular housekeeping task list. No manual intervention is needed for recurring event poll creation.

We use Poetry for local development dependencies:
poetry install

For runtime-only dependencies (for lean environments):
poetry install --only main

to create a venv

# Test with
`poetry run tox -r`

For debugging against the firestore emulator, in the vscode env setting:

`"FIRESTORE_EMULATOR_HOST": "127.0.0.1:8080",`

For VS Code Test Explorer runs, env vars are loaded from `.env.test` via `.vscode/settings.json`.
Current defaults:

`FIRESTORE_EMULATOR_HOST=127.0.0.1:8080`

`GOOGLE_CLOUD_PROJECT=demo-firebase-sub-integration`

# Bootstrap CLI (admin and role setup)

Use these commands to initialize a new local/emulator project without manual Firestore edits.

Create or update a first admin user and grant default admin permissions:

```bash
poetry run python firebase_sub/cli/bootstrap.py create-admin --uid <auth-uid> --name "Admin User" --email admin@example.com
```

Preview changes without writing:

```bash
poetry run python firebase_sub/cli/bootstrap.py create-admin --uid <auth-uid> --dry-run
```

Grant one or more role docs explicitly:

```bash
poetry run python firebase_sub/cli/bootstrap.py grant-role --uid <auth-uid> --role canChat --role canCreatePoll
```

When `FIRESTORE_EMULATOR_HOST` is set, this command targets the emulator and uses `GOOGLE_CLOUD_PROJECT` for the project id.

# Backup and Restore Runbook

Use backup before any destructive maintenance work.

Create a backup and manifest:

```bash
poetry run python firebase_sub/cli/backup.py --outfile backup.json
```

This produces:
1. `backup.json` with path-aware records (schema v2)
2. `backup.json.manifest.json` with checksum, counts, and timing metadata

Restore planning only (no writes):

```bash
poetry run python firebase_sub/cli/restore.py --infile backup.json --manifest backup.json.manifest.json --dry-run
```

Restore only user-scoped documents:

```bash
poetry run python firebase_sub/cli/restore.py --infile backup.json --manifest backup.json.manifest.json --uid <uid>
```

Restore only specific collections:

```bash
poetry run python firebase_sub/cli/restore.py --infile backup.json --manifest backup.json.manifest.json --allow-collection users --allow-collection user-public
```

Exclude collections from restore:

```bash
poetry run python firebase_sub/cli/restore.py --infile backup.json --manifest backup.json.manifest.json --deny-collection roles --deny-collection chat_push_actions
```

Safety behavior for non-dry-run restores:
1. Broad unscoped restore is refused unless `--confirm-non-dry-run` is set.
2. Scoped restores (`--uid` or `--allow-collection`) do not require the confirmation flag.
3. Overlapping allow/deny collection filters are rejected.

Example of explicit broad restore confirmation:

```bash
poetry run python firebase_sub/cli/restore.py --infile backup.json --manifest backup.json.manifest.json --confirm-non-dry-run
```

Recommended pre-delete checklist:
1. Run backup and verify manifest checksum.
2. Execute a dry-run restore and inspect planned writes.
3. Validate a user-scoped restore path in emulator before production usage.

# Admin Delete User Runbook (Dual-Gate)

Admin Auth deletion is intentionally guarded by two independent controls:
1. Environment gate: `ENABLE_ADMIN_DELETE_REQUESTS=true`
2. Runtime gate: `--enable-real-auth-delete`

Both must be enabled before real Auth deletion can happen.

## Start in dry-run mode

Dry-run validates preconditions and writes audit records, but does not delete Auth users:

```bash
ENABLE_ADMIN_DELETE_REQUESTS=true poetry run python firebase_sub/cli/sub_events.py --no-enable-real-auth-delete --loglevel info
```

## Enable real Auth deletion

Enable both gates only after dry-run outcomes look healthy:

```bash
ENABLE_ADMIN_DELETE_REQUESTS=true poetry run python firebase_sub/cli/sub_events.py --enable-real-auth-delete --loglevel info
```

## Emergency rollback / pause

Set kill-switch document `system_config/admin_delete`:

```json
{
	"paused": true,
	"reason": "on-call pause",
	"pausedAt": "<server timestamp>"
}
```

When `paused=true`, pending requests are skipped immediately with no destructive action.

## Observability counters

The worker emits Firestore counters in `admin_delete_request_metrics`:
1. `global` document (all-time aggregates)
2. `daily-YYYY-MM-DD` documents (daily aggregates)

Key outcomes to alert on:
1. `outcomes.auth_delete_failed`
2. `outcomes.auth_delete_blocked`

Audit history remains immutable in `admin_delete_request_audit` and should be used for incident forensics.

Read current counters from CLI:

```bash
poetry run python firebase_sub/cli/admin_delete_metrics.py
```

Run a one-command on-call preflight (dual gate + kill-switch + fail/block counters):

```bash
ENABLE_ADMIN_DELETE_REQUESTS=true poetry run python firebase_sub/cli/admin_delete_metrics.py --preflight --enable-real-auth-delete
```

Preflight exit codes:
1. `0`: ready (effective real delete is on, kill-switch not paused)
2. `2`: not ready (effective real delete is off)
3. `3`: blocked (kill-switch paused)

Read a specific UTC day:

```bash
poetry run python firebase_sub/cli/admin_delete_metrics.py --day 2026-05-12
```

Create a per-user recovery snapshot before admin deletion:

```bash
poetry run python firebase_sub/cli/snapshot_user.py --uid <uid> --outfile user-<uid>-snapshot.json
```

If the Auth account has already been removed and you still need Firestore recovery data:

```bash
poetry run python firebase_sub/cli/snapshot_user.py --uid <uid> --outfile user-<uid>-snapshot.json --allow-missing-auth
```

# Deisgn Overview




# Running/Building the docker

## Build process
The Docker image uses a multi-stage build process:
1. **Builder stage**: Poetry builds a Python wheel from `pyproject.toml` and the source code
2. **Runtime stage**: Dependencies are installed via Poetry (from `poetry.lock`), then the wheel is installed via pip

This approach keeps the package installation clean and production-ready without embedding the full source tree in the image.

To build/run the docker on the pi:
cd ~/home/git/src/github.com/cbehopkins/pubnightpicker
sudo docker build -t sub_events .

For debug:
`sudo docker run -it --rm --name pubnight_sub -e MAILTRAP_TOKEN="$MAILTRAP_TOKEN" -e FIREBASE_CRED_PATH=/usr/src/app/cred.json -v "$PWD/cred.json:/usr/src/app/cred.json:ro" sub_events`

Persist:
`sudo docker run -d --restart unless-stopped --name pubnight_sub -e MAILTRAP_TOKEN="$MAILTRAP_TOKEN" -e FIREBASE_CRED_PATH=/usr/src/app/cred.json -v "$PWD/cred.json:/usr/src/app/cred.json:ro" sub_events`

# Restarting the deployed service (compose repo)
If you deploy with the separate compose repo, this is the low-downtime flow we used:

1. Build a fresh local image on the same Docker host that runs the service:

```bash
cd .../github.com/cbehopkins/pubnightpicker/firebase_sub
sudo docker build -t sub_events .
```

2. Recreate only the `pubnight_sub` service from the compose repo (This is my private compose repo - no peeking):

```bash
cd .../home_compose/pubnight_sub
sudo docker compose up -d --no-deps --force-recreate pubnight_sub
```

3. Verify status and logs:

```bash
sudo docker compose ps pubnight_sub
sudo docker compose logs --tail 100 -f pubnight_sub
```

If you see `container name "/pubnight_sub" is already in use`, remove the old manually-run container and retry:

```bash
sudo docker rm -f pubnight_sub
sudo docker compose up -d --no-deps --force-recreate pubnight_sub
```

This conflict happens when an older container with the same name exists but was not created by Docker Compose.

Check for stopped
`sudo docker ps -a`
and prune
`sudo docker container prune`

# Notification Mirror Runbook

## Start the service

Local CLI:

```bash
poetry run python firebase_sub/cli/sub_events.py --loglevel info
```

Push can be toggled independently from email dummy mode:

```bash
# Real push, dummy email
poetry run python firebase_sub/cli/sub_events.py --dummy-email --no-dummy-push --loglevel info

# Dummy push, real email
poetry run python firebase_sub/cli/sub_events.py --no-dummy-email --dummy-push --loglevel info
```

Docker:

```bash
sudo docker run -it --rm --name pubnight_sub -e MAILTRAP_TOKEN="$MAILTRAP_TOKEN" -e FIREBASE_CRED_PATH=/usr/src/app/cred.json -v "$PWD/cred.json:/usr/src/app/cred.json:ro" sub_events
```

## Manual diagnostics verification

1. In Firestore, create/update document `notification_req/diagnostics` with field `manual`.
2. Set `manual = 123` and verify `notification_ack/diagnostics.manual` becomes `123`.
3. Update `manual = 456` and verify `notification_ack/diagnostics.manual` becomes `456`.
4. Add another request key and verify the ack document keeps existing keys and adds only missing/changed keys.

Example script against emulator or configured project:

```bash
python - <<'PY'
from firebase_admin import credentials, firestore, initialize_app

initialize_app(credentials.Certificate("cred.json"))
db = firestore.client()

db.collection("notification_req").document("diagnostics").set({"manual": 123}, merge=True)
print("ack after 123:", db.collection("notification_ack").document("diagnostics").get().to_dict())

db.collection("notification_req").document("diagnostics").set({"manual": 456}, merge=True)
print("ack after 456:", db.collection("notification_ack").document("diagnostics").get().to_dict())
PY
```
