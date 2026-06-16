"""Fail-fast utilities for worker thread crashes.

If a background worker thread terminates with an unhandled exception, the
service should exit so external process monitors can restart it.
"""

import logging
import os
import threading
from collections.abc import Callable
from typing import Any

_log = logging.getLogger(__name__)


def install_fail_fast_thread_excepthook(
    *,
    exit_func: Callable[[int], None] | None = None,
) -> Callable[[], None]:
    """Install a fail-fast ``threading.excepthook`` and return a restore callback.

    The installed hook logs the worker-thread crash and terminates the process.
    """
    previous_hook = threading.excepthook
    terminate = exit_func or os._exit

    def _hook(args: Any) -> None:
        _log.critical(
            "Fatal worker thread crash in %s",
            getattr(getattr(args, "thread", None), "name", "unknown-thread"),
            exc_info=(
                getattr(args, "exc_type", Exception),
                getattr(args, "exc_value", None),
                getattr(args, "exc_traceback", None),
            ),
        )
        terminate(1)

    threading.excepthook = _hook

    def _restore() -> None:
        threading.excepthook = previous_hook

    return _restore
