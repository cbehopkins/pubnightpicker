"""E2E integration regression test for frontend/backend weekday mapping.

Expected behavior:
- Frontend recurrence form uses Monday-based indices (Wednesday => 2).
- Backend must interpret persisted recurrence weekday values so that user intent
    is preserved.

For "3rd Wednesday in May", the computed next occurrence must be Wednesday.
"""

from __future__ import annotations

from datetime import date
from pathlib import Path

import pytest
from google.cloud.firestore_v1.client import Client

from firebase_sub.database.housekeeping_tasks import maintain_event_recurrence_polls

REPO_ROOT = Path(__file__).resolve().parents[3]
PUB_FORM_PATH = REPO_ROOT / "react" / "src" / "components" / "pages" / "PubForm.js"



def _frontend_weekday_value_for_wednesday() -> int:
    """Read frontend weekday option mapping directly from PubForm source."""
    source = PUB_FORM_PATH.read_text(encoding="utf-8")
    marker = '["2", "Wednesday"]'
    if marker not in source:
        raise AssertionError(
            "Expected frontend weekday mapping '[\"2\", \"Wednesday\"]' was not found"
        )
    return 2


@pytest.mark.integration
@pytest.mark.e2e
def test_yearly_third_wednesday_from_frontend_value_materializes_wednesday_date(
    firestore_client: Client,
) -> None:
    """A frontend 'Wednesday' selection should materialize as Wednesday."""
    frontend_wednesday_value = _frontend_weekday_value_for_wednesday()

    event_id = "e2e-weekday-mismatch-event"
    firestore_client.collection("pubs").document(event_id).set(
        {
            "name": "E2E Recurrence Mismatch Event",
            "venueType": "event",
            "recurrence": {
                "frequency": "yearly",
                "start_date": "2026-01-01",
                "month": 5,
                "nth": 3,
                # Frontend currently posts Wednesday as 2.
                "weekday": frontend_wednesday_value,
                "interval": 1,
            },
        },
        merge=True,
    )

    # Run backend materialization pass that computes next_occurrence_date.
    maintain_event_recurrence_polls(firestore_client, today=date(2026, 1, 1))

    venue_data = (
        firestore_client.collection("pubs").document(event_id).get().to_dict() or {}
    )
    next_occurrence = venue_data.get("next_occurrence_date")

    # 2026-05-20 is the 3rd Wednesday in May 2026.
    assert next_occurrence == "2026-05-20"

    # This is the day that users should see in the frontend "Next Event" section.
    assert date.fromisoformat(next_occurrence).strftime("%A") == "Wednesday"
