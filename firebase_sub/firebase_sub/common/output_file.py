import json
import logging
from pathlib import Path

from firebase_sub.common.serialization import convert_datetimes

_log = logging.getLogger(__name__)


class OutputFile:
    def __init__(self, path: Path) -> None:
        self.path = path
        self.file = None
        self.separator = ""

    def write_dict(self, collection: str, doc_id: str, data: dict) -> None:
        assert self.file is not None, "Output file not opened"
        record = {
            "collection": collection,
            "id": doc_id,
            "data": convert_datetimes(data),
        }
        try:
            json_str = json.dumps(record, ensure_ascii=False)
        except TypeError as e:
            _log.error(
                f"Error serializing document {doc_id} in collection {collection}: {e}"
            )
            return
        self.file.write(self.separator + json_str)
        self.separator = ",\n"
        self.file.flush()

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
