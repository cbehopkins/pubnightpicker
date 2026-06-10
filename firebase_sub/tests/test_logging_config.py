import logging

from firebase_sub.common.logging import (
    DEFAULT_LOG_DATE_FORMAT,
    DEFAULT_LOG_FORMAT,
    configure_logging,
)


def test_configure_logging_sets_timestamped_formatter(tmp_path):
    logfile = tmp_path / "app.log"

    configure_logging(logging.INFO, logfile)

    root_logger = logging.getLogger()
    assert root_logger.handlers

    formatter = root_logger.handlers[0].formatter
    assert formatter is not None
    assert formatter._fmt == DEFAULT_LOG_FORMAT
    assert formatter.datefmt == DEFAULT_LOG_DATE_FORMAT
    assert "%(asctime)s" in formatter._fmt
