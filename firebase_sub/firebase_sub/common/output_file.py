import json
import logging
from datetime import datetime
from pathlib import Path
from typing import Any

from firebase_sub.common.serialization import convert_datetimes

_log = logging.getLogger(__name__)


class OutputFile:
    def __init__(self, path: Path) -> None:
        self.path = path
        self.file = None
        self.separator = ""

    def write_document(
        self,
        path: str,
        data: dict[str, Any],
        *,
        collection: str | None = None,
        doc_id: str | None = None,
        create_time: datetime | None = None,
        update_time: datetime | None = None,
    ) -> None:
        assert self.file is not None, "Output file not opened"
        path_parts = [part for part in path.split("/") if part]
        if len(path_parts) < 2 or len(path_parts) % 2 != 0:
            raise ValueError(f"Document path must have even segments: {path}")

        resolved_collection = collection or path_parts[-2]
        resolved_doc_id = doc_id or path_parts[-1]
        record = {
            "schema_version": 2,
            "path": "/".join(path_parts),
            "collection": resolved_collection,
            "id": resolved_doc_id,
            "data": convert_datetimes(data),
        }
        if create_time is not None:
            record["create_time"] = create_time
        if update_time is not None:
            record["update_time"] = update_time
        try:
            json_str = json.dumps(convert_datetimes(record), ensure_ascii=False)
        except TypeError as e:
            _log.error(
                "Error serializing document %s in collection %s: %s",
                resolved_doc_id,
                resolved_collection,
                e,
            )
            return
        self.file.write(self.separator + json_str)
        self.separator = ",\n"
        self.file.flush()

    def write_dict(self, collection: str, doc_id: str, data: dict) -> None:
        self.write_document(path=f"{collection}/{doc_id}", data=data)

    def close(self) -> None:
        assert self.file is not None, "Output file not opened"
        self.file.write("\n]\n")
        self.file.close()
        self.file = None

    def __enter__(self) -> "OutputFile":
        self.file = open(self.path, "w", encoding="utf-8")
        self.file.write("[\n")
        return self

    def __exit__(self, exc_type, exc_val, exc_tb) -> None:
        self.close()
