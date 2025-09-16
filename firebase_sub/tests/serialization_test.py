import json
from datetime import datetime, timezone

import pytest
from google.api_core.datetime_helpers import DatetimeWithNanoseconds

from firebase_sub.common.serialization import convert_datetimes, restore_datetimes


def test_roundtrip_datetime():
    dt = datetime(2024, 6, 1, 12, 34, 56, 789000, tzinfo=timezone.utc)
    dtns = DatetimeWithNanoseconds(2024, 6, 1, 12, 34, 56, 789000, tzinfo=timezone.utc)
    data = {
        "dt": dt,
        "dtns": dtns,
        "list": [dt, dtns],
        "nested": {"a": dt, "b": dtns},
    }
    # Convert datetimes to serializable
    serializable = convert_datetimes(data)
    # Serialize to JSON and back
    json_str = json.dumps(serializable)
    loaded = json.loads(json_str)
    # Restore datetimes
    restored = restore_datetimes(loaded)
    # Check types and values
    assert isinstance(restored["dt"], datetime)
    assert isinstance(restored["dtns"], datetime)
    assert restored["dt"] == dt
    assert restored["dtns"] == dtns
    assert isinstance(restored["list"][0], datetime)
    assert isinstance(restored["list"][1], datetime)
    assert restored["list"][0] == dt
    assert restored["list"][1] == dtns
    assert isinstance(restored["nested"]["a"], datetime)
    assert isinstance(restored["nested"]["b"], datetime)
    assert restored["nested"]["a"] == dt
    assert restored["nested"]["b"] == dtns


def test_reverse_roundtrip():
    # Start with ISO string
    iso = "2024-06-01T12:34:56.789000+00:00"
    data = {"dt": iso, "list": [iso], "nested": {"a": iso}}
    restored = restore_datetimes(data)
    serializable = convert_datetimes(restored)
    # Should be ISO string again
    assert serializable["dt"] == iso
    assert serializable["list"][0] == iso
    assert serializable["nested"]["a"] == iso
