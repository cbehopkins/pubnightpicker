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

Run this procedure in **Cloud Shell** after selecting the intended project. It
uses Cloud Shell's authenticated identity, reads only the `polls` and `pubs`
collections, and prints aggregate counts (no document contents or IDs). Do not
run it from a workstation configured with production credentials.

```bash
PROJECT_ID="the-exact-project-id-to-audit"
gcloud config set project "$PROJECT_ID"
test "$(gcloud config get-value project 2>/dev/null)" = "$PROJECT_ID"
python3 -m pip install --user google-cloud-firestore
GOOGLE_CLOUD_PROJECT="$PROJECT_ID" python3 - <<'PY'
import os
from collections import Counter
from google.cloud import firestore

project_id = os.environ["GOOGLE_CLOUD_PROJECT"]
db = firestore.Client(project=project_id)
venue_ids = {ref.id for ref in db.collection("pubs").list_documents()}
counts = Counter()

for snapshot in db.collection("polls").stream():
    counts["meets_total"] += 1
    data = snapshot.to_dict() or {}
    selected = data.get("selected")
    if "selected" not in data or selected is None:
        counts["selected_missing_or_null"] += 1
    elif not isinstance(selected, str):
        counts["selected_not_string"] += 1
    elif selected == "":
        counts["selected_empty"] += 1
    elif selected == "any":
        counts["selected_any"] += 1
    else:
        counts["selected_nonempty"] += 1
        if selected not in venue_ids:
            counts["selected_venue_missing"] += 1

    # Per the agreed invariant, "any" is treated as complete for this audit.
    expected = isinstance(selected, str) and selected != ""
    if selected == "any":
        expected = True
    completed = data.get("completed")
    if not isinstance(completed, bool):
        counts["legacy_completed_missing_or_invalid"] += 1
    elif completed != expected:
        counts["legacy_completed_mismatch"] += 1

print(f"Firestore project: {project_id}")
for key in (
    "meets_total", "selected_missing_or_null", "selected_empty",
    "selected_not_string", "selected_any", "selected_nonempty",
    "selected_venue_missing", "legacy_completed_missing_or_invalid",
    "legacy_completed_mismatch",
):
    print(f"{key}: {counts[key]}")
PY
```

Review the project ID and aggregate report before rollout. The report counts:

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
