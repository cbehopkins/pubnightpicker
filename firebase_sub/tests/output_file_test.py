import json
import tempfile
from datetime import datetime, timezone
from pathlib import Path

from firebase_sub.common.output_file import OutputFile


def test_output_file_basic():
    with tempfile.TemporaryDirectory() as tmpdir:
        outpath = Path(tmpdir) / "test.json"
        with OutputFile(outpath) as out:
            out.write_dict("col1", "id1", {"foo": "bar"})
            out.write_dict("col2", "id2", {"num": 42, "lst": [1, 2, 3]})
        # Read and check file
        with open(outpath, "r", encoding="utf-8") as f:
            data = json.load(f)
        assert isinstance(data, list)
        assert data[0]["collection"] == "col1"
        assert data[0]["id"] == "id1"
        assert data[0]["data"] == {"foo": "bar"}
        assert data[1]["collection"] == "col2"
        assert data[1]["id"] == "id2"
        assert data[1]["data"] == {"num": 42, "lst": [1, 2, 3]}


def test_output_file_datetime():
    dt = datetime(2024, 6, 1, 12, 34, 56, 789000, tzinfo=timezone.utc)
    with tempfile.TemporaryDirectory() as tmpdir:
        outpath = Path(tmpdir) / "test_dt.json"
        with OutputFile(outpath) as out:
            out.write_dict("col", "id", {"when": dt})
        with open(outpath, "r", encoding="utf-8") as f:
            data = json.load(f)
        assert isinstance(data, list)
        assert data[0]["data"]["when"] == dt.isoformat()
