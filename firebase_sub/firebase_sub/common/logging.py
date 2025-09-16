import logging
from pathlib import Path
from typing import Any


def log_level_to_int(level: str | int) -> int:
    try:
        return int(level)
    except ValueError:
        assert isinstance(level, str), "Level must be a string by now..."
        return int(logging.getLevelNamesMapping().get(level, logging.INFO))


def configure_logging(log_level: str | int, logfile: str | Path | None):
    logging_config: dict[str, Any]
    logging_config = {"level": log_level}
    if logfile:
        print(f"Logging to {logfile}")
        logging_config["filename"] = logfile
        logging_config["encoding"] = "utf-8"

    logging.basicConfig(**logging_config)
    logging.getLogger("google.api_core.bidi").setLevel(logging.WARNING)
