# Initial Group Migration

This standalone Python command creates the initial public Group and assigns its
Firestore-generated ID to every existing Meet in the historical `polls`
collection. It does not depend on the retired `firebase_sub` project.

## Run

Install this utility's dependencies:

```powershell
cd group_migration
poetry install
```

Run the command with credentials for the intended Firebase project:

```powershell
poetry run python -m group_migration.migrate_meets --name ampubnight
```

Use `GOOGLE_APPLICATION_CREDENTIALS` or Application Default Credentials and set
`GOOGLE_CLOUD_PROJECT` to select the project. For emulator use, also set
`FIRESTORE_EMULATOR_HOST`; the command then uses emulator credentials.

## Operator Safety

- Schedule a supervised quiet period and ensure there are no Meet writers for
  the duration. This migration does not coordinate concurrent writes.
- The command refuses to write if any document exists in `groups`, regardless
  of its name. Do not bootstrap a Group in the migration target first.
- The migration is not atomic across Meets. A failure reports the generated
  Group ID and the number of Meets updated. A later normal invocation refuses
  because that Group exists; inspect and recover explicitly before continuing.
- Do not blindly delete the Group before rerunning. Meets already updated may
  retain its ID and conflict with a newly generated Group ID.
- This utility does not roll back, resume, or delete data automatically.

## Tests

The command-boundary integration tests use a unique `demo-*` project ID per
test. Start the Firestore emulator and set `FIRESTORE_EMULATOR_HOST` before
running them:

```powershell
poetry run pytest -m integration
```

No production migration is run by the test suite or by this change.